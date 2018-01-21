package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-github/github"
	"github.com/nlopes/slack"
	"golang.org/x/oauth2"
)

type TicketBot struct {
	ctx    context.Context
	client *github.Client
	rtm    *slack.RTM
}

func NewTicketBot() TicketBot {
	fmt.Println("NEW TICKET BOT")
	// Init slack
	api := slack.New(os.Getenv("SLACK_APP_KEY"))
	logger := log.New(os.Stdout, "slack-bot: ", log.Lshortfile|log.LstdFlags)
	slack.SetLogger(logger)
	api.SetDebug(true)
	rtm := api.NewRTM()

	// Init github
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_KEY")},
	)

	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Set initialized apis
	tb := TicketBot{
		ctx:    ctx,
		client: client,
		rtm:    rtm,
	}

	return tb
}

func (tb *TicketBot) getTicketNumbers(m string) []int {
	re, _ := regexp.Compile("(![0-9]+)")
	bangRe, _ := regexp.Compile("(!)")
	matches := re.FindAllString(m, -1)
	result := []int{}

	for _, match := range matches {
		ticketNumber := bangRe.ReplaceAllString(match, "")
		n, _ := strconv.Atoi(ticketNumber)
		result = append(result, n)
	}

	return result
}

func (tb *TicketBot) getIssues(tNums []int, issues chan *github.Issue, errc chan error) {
	defer close(issues)
	defer close(errc)

	for _, n := range tNums {
		issue, _, err := tb.client.Issues.Get(tb.ctx, "soxhub", "qa", n)
		if err != nil {
			errc <- err
		} else {
			issues <- issue
		}
	}
}

func (tb *TicketBot) digestIssues(c <-chan *github.Issue) []string {
	messages := []string{}

	for issue := range c {
		messages = append(messages, buildIssueResponseMessage(issue))
	}

	return messages
}

func buildIssueResponseMessage(i *github.Issue) string {
	num := strconv.Itoa(*i.Number)
	us := mapGHUser(i.Assignees)
	assignees := strings.Join(us, ", ")

	return "#" + num + " - " + i.GetTitle() + " \n" + i.GetHTMLURL() + "\n" + "Assignees: " + assignees
}

func mapGHUser(users []*github.User) []string {
	collection := make([]string, len(users))

	for i, v := range users {
		collection[i] = *v.Login
	}

	return collection
}

func (tb *TicketBot) resolveIssues(ev *slack.MessageEvent, issues chan *github.Issue, errc chan error) {
	select {
	case err := <-errc:
		fmt.Println("Error", err)
	default:
		ms := tb.digestIssues(issues)
		for _, m := range ms {
			tb.rtm.SendMessage(tb.rtm.NewOutgoingMessage(m, ev.Channel))
		}
	}
}

func (tb *TicketBot) handleEvents() {
	fmt.Println("handleEvents")
	go tb.rtm.ManageConnection()

	for msg := range tb.rtm.IncomingEvents {
		switch ev := msg.Data.(type) {
		case *slack.HelloEvent:
			// Hello and Connection event should display the same message, give info about bot `!<num>`
			fmt.Println("Hello Event..")

		case *slack.ConnectedEvent:
			fmt.Println("Connection Event..")

		case *slack.MessageEvent:
			ticketNums := tb.getTicketNumbers(ev.Text)
			issues := make(chan *github.Issue)
			errc := make(chan error)

			go tb.getIssues(ticketNums, issues, errc)
			go tb.resolveIssues(ev, issues, errc)

		case *slack.RTMError:
			fmt.Printf("Error: %s\n", ev.Error())

		case *slack.InvalidAuthEvent:
			fmt.Printf("Invalid credentials")
			return

		default:
			// Ignore other events..
		}
	}
}

func main() {
	tb := NewTicketBot()
	tb.handleEvents()
}
