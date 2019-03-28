package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"

	// "net/http"
	// _ "net/http/pprof"
	"net/mail"
	"os"
	"path"
	"strings"
	"sync"
	"unicode"

	helpscout "github.com/BrianLeishman/go-helpscout"
	imap "github.com/BrianLeishman/go-imap"
	homedir "github.com/mitchellh/go-homedir"
	pngquant "github.com/yusukebe/go-pngquant"
	pb "gopkg.in/cheggaaa/pb.v1"
	"gopkg.in/gographics/imagick.v3/imagick"
	yaml "gopkg.in/yaml.v2"
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

type progress struct {
	Account string `yaml:"account"`
	Folder  string `yaml:"folder"`
	UIDs    []int  `yaml:"uids"`
}

func main() {

	// go func() {
	// 	log.Println(http.ListenAndServe("localhost:6060", nil))
	// }()

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

	forceRestart := flag.Bool("force-restart", false, "ignore any resume data/flags and force a full restart of the import")
	ignoreResumeData := flag.Bool("ignore-resume-data", false, "ignores any resume data")

	chunkSize := flag.Int("chunk-size", 16, "how many emails to download per chunk (how many bodies to ask the server for per fetch request)")
	chunks := flag.Int("chunks", 4, "max number of simultaneous chunks (e.g. with 4: if 4 chunks have emails uploading to Help Scout, don't download another chunk yet)")

	test := flag.Bool("t", false, "test run (don't actually import anything to Help Scout)")

	flag.Parse()

	profFileName, err := homedir.Expand("~/.imap2helpscout")
	if err != nil {
		log.Fatalln(err)
	}

	progFile, err := os.OpenFile(profFileName, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		log.Fatalln(err)
	}
	defer progFile.Close()

	progs := make([]progress, 0)
	progsMx := sync.Mutex{}
	buf := bytes.NewBuffer(nil)
	io.Copy(buf, progFile)
	err = yaml.Unmarshal(buf.Bytes(), &progs)
	if err != nil {
		log.Fatalln("could not parse progress file (~/.imap2helpscout):", err)
	}

	progExists := false
	progI := 0

	for i, p := range progs {
		if p.Account == *username {
			if *ignoreResumeData {
				progs = append(progs[:i], progs[i+1:]...)
			} else {
				progI = i
				progExists = true
			}
			break
		}
	}

	if *forceRestart {
		*resumeFolder = ""
		*resumeUID = 0
	} else if progExists {
		*resumeFolder = progs[progI].Folder
	}

	if !progExists {
		progI = len(progs)
		progs = append(progs, progress{Account: *username})
	}

	writeProgs := func() {
		err := progFile.Truncate(0)
		if err != nil {
			log.Println(err)
		}

		b, err := yaml.Marshal(progs)
		if err != nil {
			log.Println(err)
		}

		_, err = progFile.WriteAt(b, 0)
		if err != nil {
			log.Println(err)
		}

		err = progFile.Sync()
		if err != nil {
			log.Println(err)
		}
	}

	if len(*username) == 0 {
		log.Fatal("your IMAP username is required (-u)")
	}
	if len(*password) == 0 {
		log.Fatal("your IMAP password is required (-p)")
	}
	if len(*server) == 0 {
		log.Fatal("your IMAP host is required (-h)")
	}
	if *port == 0 {
		log.Fatal("your IMAP port is required (-P)")
	}

	if len(*appID) == 0 {
		log.Fatal("your Help Scout App ID is required (-a)")
	}
	if len(*appSecret) == 0 {
		log.Fatal("your Help Scout App Secret is required (-s)")
	}

	if *chunkSize < 1 {
		log.Fatal("chunk size can't be smaller than 1")
	}
	if *chunks < 1 {
		log.Fatal("chunks can't be smaller than 1")
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

	fmt.Println("Getting some things ready, one sec...")

	im, err := imap.New(*username, *password, *server, *port)
	check("Failed to connect to IMAP server", err)
	defer im.Close()

	folders, err := im.GetFolders()
	check("Failed to get folders", err)

	resumeFolderFound := false
	if !*forceRestart && *resumeFolder != "" {
		l := strings.ToLower(*resumeFolder)
		for i, f := range folders {
			if strings.ToLower(f) == l {
				folders = folders[i:]
				*resumeFolder = f
				resumeFolderFound = true
				break
			}
		}
		if !resumeFolderFound {
			log.Fatal("resume folder doesn't exist on server")
		}
	}

	var count int
	count, err = im.GetTotalEmailCountStartingFromExcluding(*resumeFolder, excludedFolders)
	check("Failed to get total email count", err)

	bar := pb.StartNew(count)

	hs, err := helpscout.New(*appID, *appSecret)
	check("Failed to connect to Help Scout", err)

	err = hs.SelectMailbox(*username)
	check("Failed to select mailbox", err)

	imagick.Initialize()
	defer imagick.Terminate()

	emailsCh := make(chan struct{}, (*chunkSize)*(*chunks))

	for _, f := range folders {
		exclude := false
		for _, ef := range excludedFolders {
			if strings.HasPrefix(f, ef) {
				exclude = true
				break
			}
		}
		if exclude {
			continue
		}

		err = im.SelectFolder(f)
		check("failed to select folder", err)

		uids, err := im.GetUIDs(*search)
		check("failed to get uids", err)

		if !*forceRestart && progExists && len(progs[progI].UIDs) != 0 {
			progUIDsMap := make(map[int]struct{}, len(progs[progI].UIDs))
			for _, uid := range progs[progI].UIDs {
				progUIDsMap[uid] = struct{}{}
			}
			newUIDs := make([]int, 0, len(uids))
			for _, u := range uids {
				if _, ok := progUIDsMap[u]; !ok {
					newUIDs = append(newUIDs, u)
				}
			}
			progExists = false
			uids = newUIDs
			*resumeUID = 0
		} else if !*forceRestart && *resumeUID != 0 && *resumeFolder == f {
			skipped := 0
			for i, u := range uids {
				if u >= *resumeUID {
					uids = uids[i:]
					*resumeUID = 0
					skipped++
					break
				}
			}
			// bar = pb.StartNew(count - skipped)
			bar.SetTotal(count - skipped)
		} else {
			progs[progI].Folder = f
			progs[progI].UIDs = make([]int, 0, len(uids))
			writeProgs()
		}

		for i := 0; i < len(uids); i += *chunkSize {

			func() {
				var u []int
				if i+*chunkSize > len(uids) {
					u = uids[i:]
				} else {
					u = uids[i : i+*chunkSize]
				}

				defer bar.Add(len(u))

				emails, err := im.GetEmails(u...)
				check("failed to get emails", err)

				// e should be only one email, but it could also be no elements
				// since every UID searched is not guaranteed to return an email
				for _, e := range emails {
					wg.Add(1)
					emailsCh <- struct{}{}
					go func(e *imap.Email, f string) {
						defer func() {
							progsMx.Lock()

							progs[progI].UIDs = append(progs[progI].UIDs, e.UID)
							writeProgs()

							progsMx.Unlock()

							<-emailsCh
							wg.Done()
						}()

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
						if !*test {
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
						}
						<-helpScoutCh
						if err != nil {
							return
						}
						if len(e.Attachments) != 0 {
							for _, a := range e.Attachments {
								wg.Add(1)
								go func(a imap.Attachment) {
									var err error
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

									if !*test {
										err = hs.UploadAttachment(conversationID, threadID, a.Name, a.MimeType, a.Content)
									}
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

	if bar != nil {
		bar.Finish()
	}

	progs = append(progs[:progI], progs[progI+1:]...)
	writeProgs()

}
