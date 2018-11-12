package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/BrianLeishman/go-helpscout"
	"github.com/BrianLeishman/go-imap"
	"github.com/yusukebe/go-pngquant"
	"gopkg.in/cheggaaa/pb.v1"
	"gopkg.in/gographics/imagick.v2/imagick"
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

func main() {
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

	helpScoutCh := make(chan struct{}, 3)
	wg := sync.WaitGroup{}

	imap.Verbose = *verbose
	imap.SkipResponses = !*moreVerbose

	helpscout.Verbose = *verbose
	helpscout.ShowPostData = *moreVerbose
	helpscout.ShowResponse = *moreVerbose
	helpscout.RateLimitMinute = 800
	// helpscout.RetryCount = 0

	started := true
	if len(*resumeFolder) != 0 && *resumeUID != 0 {
		started = false
	}

	fmt.Println("Getting some things ready, one sec...")

	im, err := imap.New(*username, *password, *server, *port)
	check("Failed to connect to IMAP server", err)
	defer im.Close()

	var count int
	// if !*verbose {
	count, err = im.GetTotalEmailCountStartingFromExcluding(*resumeFolder, excludedFolders)
	check("Failed to get total email count", err)
	// }

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

		uids, err := im.GetUIDs("ALL")
		check("Failed to get uids", err)

		for _, u := range uids {
			if !started {
				if u >= *resumeUID {
					bar = pb.StartNew(count)
					started = true
				} else {
					count--
					continue
				}
			}
			func() {
				// if !*verbose {
				defer bar.Increment()
				// }

				emails, err := im.GetEmails(u)
				check("Failed to get emails", err)

				// e should be only one email, but it could also be no elements
				// since every UID searched is not guaranteed to return an email
				for _, e := range emails {
					wg.Add(1)

					go func(e *imap.Email, f string) {
						defer wg.Done()

						var err error

						if len(e.From) == 0 || len(e.To) == 0 {
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
						for e := range e.To {
							to = helpscout.Customer{
								Email: &e,
								// FirstName: &n,
							}
							break
						}

						var content string
						if len(e.HTML) != 0 {
							content = e.HTML
						} else {
							content = e.Text
						}

						if len(content) == 0 {
							if len(e.Attachments) == 0 {
								return
							}
							content = "No Content"
						}

						var subject string
						if len(e.Subject) == 0 {
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
									defer wg.Done()

									if a.MimeType == "image/jpeg" {
										// Help Scout currently has a bug where some jpg's can't
										// be uploaded due to some issue with the part of their system that
										// checks if a jpg needs to be rotated or not
										// So lets just turn that jpg into a png!

										// img, err := jpeg.Decode(bytes.NewReader(a.Content))
										// check("failed to decode jpeg", err)

										// var b bytes.Buffer
										// w := bufio.NewWriter(&b)
										// err = jpeg.Encode(w, img, &jpeg.Options{
										// 	Quality: 100,
										// })
										// check("failed to encode jpeg", err)

										// w.Flush()
										// _, err = b.Read(a.Content)
										// check("failed to read jpeg into attachment content", err)

										mw := imagick.NewMagickWand()
										defer mw.Destroy()

										err = mw.ReadImageBlob(a.Content)
										check("failed to read image blob", err)

										// f, err := os.Create(a.Name)
										// check("failed to create file", err)
										// defer f.Close()

										mw.SetImageFormat("PNG")

										err = mw.StripImage()
										check("failed to strip exif", err)

										ext := path.Ext(a.Name)
										a.Name = a.Name[0:len(a.Name)-len(ext)] + ".png"
										// pngs are GIANT though compared to jpgs, so here we compress the crap out of it
										mw.ResetIterator()
										a.Content, err = pngquant.CompressBytes(mw.GetImageBlob(), "3")
										check("failed to compress png", err)
										a.MimeType = "image/png"
									}

									if len(a.Content) > 1000*1000*10 {
										// Help Scout only allows images 10MB or smaller
										// So just discard it if it's bigger (nothing we can do about it)
										return
									}

									helpScoutCh <- struct{}{}
									err := hs.UploadAttachment(conversationID, threadID, a.Name, a.MimeType, a.Content)
									<-helpScoutCh
									// if err != nil {
									// 	check("Failed to upload attachment", fmt.Errorf("uid: %d, folder: %s, attachment: %s\n%s\n%s", e.UID, f, a, e, err))
									// }
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

	// if !*verbose {
	bar.Finish()
	// } else {
	// 	log.Println("we made it!")
	// }
}
