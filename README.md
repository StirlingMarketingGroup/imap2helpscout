# Import an entire mailbox to Help Scout

> I'm afraid we don't have a readily-built importer for services like Outlook where your existing email is hosted. :/
>
> [...] to import the whole mailbox's history, you'll need to create a solution programmatically by using our API.
>
> ​	— <cite>Help Scout employee</cite>

Oh no!

I'm sure we're not the only people who would like to switch entirely over to this awesome service, and I'm also sure that we're not the only people who want to bring our old inbox and emails along with us.

But hey, if they don't have one, then I guess we can make one for them! Who knows, maybe they'll even use it in the future ;)

## Getting Started

As this tool is written in Go, you can install it the usual way Go programs are installed. If you don't already have Go installed, you can grab it from here https://golang.org/doc/install.

```shell
go install github.com/StirlingMarketingGroup/imap2helpscout
```

Also, you will need an App ID and App Secret from Help Scout, which you can get by logging into your Help Scout account and going to your profile

![1541198438118](https://d159l1kvshziji.cloudfront.net/i/KSJ/C.jpg)

Then, on the right clicking on "My Apps"

![1541198507845](https://d159l1kvshziji.cloudfront.net/i/KSd/C.jpg)

And clicking "Create My App" and following the dialog that follows.

![1541198551681](https://d159l1kvshziji.cloudfront.net/i/KST/C.jpg)

**Note:** For the redirection URL, you can just enter something random like https://google.com, because we won't be using that field (it's used for a different authorization type)

![1541198633111](https://d159l1kvshziji.cloudfront.net/i/KSn/C.jpg)

## Usage

imap2helpscout runs as a command line program, with a few basic options that's needed to get the job done. First, all the options

```shell
  -u string
        your IMAP username
  -p string
        your IMAP password
  -h string
        your IMAP connection host
  -P int
        your IMAP conneciton port
  -a string
        your Help Scout App ID
  -s string
        your Help Scout App Secret
  -v    verbose; prints all commands to the IMAP server and to the Help Scout API
  -vv	ULTRA verbose; prints everything above AND *every* response from both the IMAP server and the Help Scout API, including all post data to the Help Scout API (even attachments encoded as base64 in the post data)
```

All of that together actually being used will look a little something like this (password, App ID & Secret are obviously not real, but you can try)

```shel
imap2helpscout -u brian@stumpyinc.com -p 'totallylegitpassword' -h imap.gmail.com -P 993 -a d6fa720430e2fde64a94ab427f7e5a17 -s 45303bcebc49241787fd9cb39bd0731f
Getting some things ready, one sec...
1982 / 137976 [=>---------------------------------------------------]   1.44% 20h14m38s
```

You'll see that it does say there "20h14m38s" remaining (!), this tool is *not* very quick. I would like it to be, but because of mostly IMAP limitations (threading this workload between multiple IMAP connections does not play nicely with services like Rackspace), so that's about as fast as we can get it. But that's for 137,976 emails (!!), so your mileage may vary.

## Built With

- [BrianLeishman/go-imap](https://github.com/BrianLeishman/go-imap) - Simple IMAP Client Library
- [BrianLeishman/go-helpscout](https://github.com/BrianLeishman/go-helpscout) - Small Help Scout API Wrapper in Golang
- [cheggaaa/pb](https://github.com/cheggaaa/pb) - Console progress bar for Golang

## Authors

- **Brian Leishman** - [Stirling Marketing Group](https://github.com/StirlingMarketingGroup)# imap2helpscout
