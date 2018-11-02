package main

import (
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/BrianLeishman/go-helpscout"
	"github.com/BrianLeishman/go-imap"
	"gopkg.in/cheggaaa/pb.v1"
)

func check(msg string, err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	username := flag.String("u", "", "your IMAP username")
	password := flag.String("p", "", "your IMAP password")
	server := flag.String("h", "", "your IMAP connection host")
	port := flag.Int("P", 0, "your IMAP connection port")

	appID := flag.String("a", "", "your Help Scout App ID")
	appSecret := flag.String("s", "", "your Help Scout App Secret")

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

	helpScoutCh := make(chan struct{}, 8)
	wg := sync.WaitGroup{}

	imap.Verbose = *verbose
	imap.SkipResponses = !*moreVerbose

	helpscout.Verbose = *verbose
	helpscout.ShowPostData = *moreVerbose
	helpscout.ShowResponse = *moreVerbose
	helpscout.RateLimitMinute = 800

	fmt.Println("Getting some things ready, one sec...")

	im, err := imap.New(*username, *password, *server, *port)
	check("Failed to connect to IMAP server", err)
	defer im.Close()

	count, err := im.GetTotalEmailCount()
	check("Failed to get total email count", err)

	folders, err := im.GetFolders()
	check("Failed to get folders", err)

	hs, err := helpscout.New(*appID, *appSecret)
	check("Failed to connect to Help Scout", err)

	err = hs.SelectMailbox(*username)
	check("Failed to select mailbox", err)

	bar := pb.StartNew(count)

	for _, f := range folders {
		err = im.SelectFolder(f)
		check("Failed to select folder", err)

		uids, err := im.GetUIDs("ALL")
		check("Failed to get uids", err)

		for _, u := range uids {
			func() {
				defer bar.Increment()

				emails, err := im.GetEmails(u)
				check("Failed to get emails", err)

				// e should be only one email, but it could also be no elements
				// since every UID searched is not guaranteed to return an email
				for _, e := range emails {
					helpScoutCh <- struct{}{}
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
							} else {
								content = "No Content"
							}
						}

						var subject string
						if len(e.Subject) == 0 {
							subject = "No Subject"
						} else {
							subject = e.Subject
						}

						var conversationID, threadID int
						if *from.Email == *username {
							conversationID, threadID, err = hs.NewConversationWithReply(
								subject,
								to,
								e.Received,
								[]string{f},
								content,
								len(e.Attachments) != 0,
							)
							<-helpScoutCh
						} else {
							conversationID, threadID, err = hs.NewConversationWithMessage(
								subject,
								from,
								e.Received,
								[]string{f},
								content,
								len(e.Attachments) != 0,
							)
							<-helpScoutCh
						}
						check("Failed to create message", err)
						if len(e.Attachments) != 0 {
							for _, a := range e.Attachments {
								helpScoutCh <- struct{}{}
								wg.Add(1)
								go func(a imap.Attachment) {
									defer wg.Done()

									err := hs.UploadAttachment(conversationID, threadID, a.Name, a.MimeType, a.Content)
									<-helpScoutCh
									if err != nil {
										check("Failed to upload attachment", fmt.Errorf("uid: %d, folder: %s, attachment: %s\n%s\n%s", e.UID, f, a, e, err))
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

	bar.FinishPrint("We made it!")
}
