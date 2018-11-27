package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"net/mail"
	"os"
	"path"
	"strings"
	"sync"
	"unicode"

	"github.com/BrianLeishman/go-helpscout"
	"github.com/BrianLeishman/go-imap"
	"github.com/yusukebe/go-pngquant"
	"gopkg.in/cheggaaa/pb.v1"
	"gopkg.in/gographics/imagick.v2/imagick"
	"jaytaylor.com/html2text"
)

func check(msg string, err error) {
	if err != nil {
		panic(err)
	}
}

type arrayFlags []string

func (i *arrayFlags) String() string {
	return ""
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, strings.TrimSpace(value))
	return nil
}

func strEmpty(s string) bool {
	if len(s) == 0 {
		return true
	}

	r := []rune(s)
	l := len(r)

	for l > 0 {
		l--
		if !unicode.IsSpace(r[l]) {
			return false
		}
	}

	return true
}

func verifyEmailAddress(e string) (email string, ok bool) {
	if strEmpty(e) {
		return
	}

	e = strings.TrimSpace(e)

	a, err := mail.ParseAddress("<" + e + ">")
	if err != nil {
		return
	}

	if strings.IndexByte(e, '@') > 64 {
		return
	}

	return strings.ToLower(a.Address), true
}

func main() {

	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	username := flag.String("u", "", "your IMAP username")
	password := flag.String("p", "", "your IMAP password")
	server := flag.String("h", "", "your IMAP connection host")
	port := flag.Int("P", 0, "your IMAP connection port")

	appID := flag.String("a", "", "your Help Scout App ID")
	appSecret := flag.String("s", "", "your Help Scout App Secret")

	var excludedFolders arrayFlags
	flag.Var(&excludedFolders, "exclude-folder", "excluded folders")

	resumeFolder := flag.String("resume-folder", "", "what folder would you like to start from?")
	resumeUID := flag.Int("resume-uid", 0, "what email UID would you like to start from?")

	search := flag.String("search", "ALL", "the IMAP UID search string, e.g. 'ALL' or 'HEADER Message-ID <04127894@email.com>'")

	verbose := flag.Bool("v", false, "verbose")
	moreVerbose := flag.Bool("vv", false, "more verbose")

	flag.Parse()

	if len(*username) == 0 {
		fmt.Println("your IMAP username is required (-u)")
		os.Exit(1)
	}
	if len(*password) == 0 {
		fmt.Println("your IMAP password is required (-p)")
		os.Exit(1)
	}
	if len(*server) == 0 {
		fmt.Println("your IMAP host is required (-h)")
		os.Exit(1)
	}
	if *port == 0 {
		fmt.Println("your IMAP port is required (-P)")
		os.Exit(1)
	}

	if len(*appID) == 0 {
		fmt.Println("your Help Scout App ID is required (-a)")
		os.Exit(1)
	}
	if len(*appSecret) == 0 {
		fmt.Println("your Help Scout App Secret is required (-s)")
		os.Exit(1)
	}

	if *moreVerbose {
		*verbose = true
	}

	helpScoutCh := make(chan struct{}, 4)
	wg := sync.WaitGroup{}

	imap.Verbose = *verbose
	imap.SkipResponses = !*moreVerbose

	helpscout.Verbose = *verbose
	helpscout.ShowPostData = *moreVerbose
	helpscout.ShowResponse = *moreVerbose
	helpscout.RetryCount = 5

	started := true
	if len(*resumeFolder) != 0 && *resumeUID != 0 {
		started = false
	}

	fmt.Println("Getting some things ready, one sec...")

	im, err := imap.New(*username, *password, *server, *port)
	check("Failed to connect to IMAP server", err)
	defer im.Close()

	var count int
	count, err = im.GetTotalEmailCountStartingFromExcluding(*resumeFolder, excludedFolders)
	check("Failed to get total email count", err)

	folders, err := im.GetFolders()
	check("Failed to get folders", err)

	hs, err := helpscout.New(*appID, *appSecret)
	check("Failed to connect to Help Scout", err)

	err = hs.SelectMailbox(*username)
	check("Failed to select mailbox", err)

	var bar *pb.ProgressBar
	if started {
		bar = pb.StartNew(count)
	}

	imagick.Initialize()
	defer imagick.Terminate()

	for _, f := range folders {
		if !started && f != *resumeFolder {
			continue
		}

		skip := false
		for _, ef := range excludedFolders {
			if strings.HasPrefix(f, ef) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		err = im.SelectFolder(f)
		check("Failed to select folder", err)

		uids, err := im.GetUIDs(*search)
		check("Failed to get uids", err)

		for _, u := range uids {
			if !started {
				if u >= *resumeUID {
					bar = pb.StartNew(count)
					started = true
				} else {
					count--
					uids = uids[1:]
					continue
				}
			}
		}

		chunk := 16
		for i := 0; i < len(uids); i += chunk {

			func() {
				var u []int
				if i+chunk > len(uids) {
					u = uids[i:]
				} else {
					u = uids[i : i+chunk]
				}

				defer bar.Add(len(u))

				emails, err := im.GetEmails(u...)
				check("Failed to get emails", err)

				// e should be only one email, but it could also be no elements
				// since every UID searched is not guaranteed to return an email
				for _, e := range emails {
					wg.Add(1)

					go func(e *imap.Email, f string) {
						defer wg.Done()

						var err error

						if len(e.From) == 0 && len(e.To) == 0 {
							return
						}

						var from, to helpscout.Customer

						for e := range e.From {
							from = helpscout.Customer{
								Email: &e,
								// FirstName: &n,
							}
							break
						}
						var ok bool
						if from.Email == nil {
							from.Email = username
						} else {
							*from.Email, ok = verifyEmailAddress(*from.Email)
							if !ok {
								*from.Email = *username
							}
						}

						for e := range e.To {
							e, ok := verifyEmailAddress(e)
							if !ok {
								e = *username
							}
							to = helpscout.Customer{
								Email: &e,
								// FirstName: &n,
							}
							break
						}
						if to.Email == nil {
							to.Email = username
						} else {
							*to.Email, ok = verifyEmailAddress(*to.Email)
							if !ok {
								*to.Email = *username
							}
						}

						stripped, _ := html2text.FromString(e.HTML)
						var content string
						if !strEmpty(stripped) {
							content = e.HTML
						} else {
							content = e.Text
						}

						if strEmpty(content) {
							if len(e.Attachments) == 0 {
								return
							}
							content = "No Content"
						}

						var subject string
						if strEmpty(e.Subject) {
							subject = "No Subject"
						} else {
							subject = e.Subject
						}

						var conversationID, threadID int
						helpScoutCh <- struct{}{}
						if *from.Email == *username {
							conversationID, threadID, err = hs.NewConversationWithReply(
								subject,
								to,
								e.Received,
								[]string{f},
								content,
								len(e.Attachments) != 0,
							)
						} else {
							conversationID, threadID, err = hs.NewConversationWithMessage(
								subject,
								from,
								e.Received,
								[]string{f},
								content,
								len(e.Attachments) != 0,
							)
						}
						<-helpScoutCh
						if err != nil {
							return
						}
						if len(e.Attachments) != 0 {
							for _, a := range e.Attachments {
								wg.Add(1)
								go func(a imap.Attachment) {
									helpScoutCh <- struct{}{}
									defer func() { <-helpScoutCh }()
									defer wg.Done()

									if a.MimeType == "image/jpeg" {
										mw := imagick.NewMagickWand()
										defer mw.Destroy()

										err = mw.ReadImageBlob(a.Content)
										if *verbose && err != nil {
											log.Println("failed to read image blob")
											return
										}

										mw.SetImageFormat("PNG")

										err = mw.StripImage()
										if *verbose && err != nil {
											log.Println("failed to strip exif")
											return
										}

										ext := path.Ext(a.Name)
										a.Name = a.Name[0:len(a.Name)-len(ext)] + ".png"
										// pngs are GIANT though compared to jpgs, so here we compress the crap out of it
										mw.ResetIterator()
										compressed, err := pngquant.CompressBytes(mw.GetImageBlob(), "3")
										if *verbose && err != nil {
											log.Println("failed to compress png")
											return
										}
										a.Content = compressed
										a.MimeType = "image/png"
									}

									if len(a.Content) > 1000*1000*10 {
										// Help Scout only allows images 10MB or smaller
										// So just discard it if it's bigger (nothing we can do about it)
										return
									}

									err := hs.UploadAttachment(conversationID, threadID, a.Name, a.MimeType, a.Content)
									if err != nil {
										return
									}
								}(a)
							}
						}
					}(e, f)
				}
			}()
		}
	}
	wg.Wait()

	bar.Finish()

}
