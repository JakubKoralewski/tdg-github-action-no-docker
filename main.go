package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/google/go-github/v31/github"
	"github.com/ribtoks/tdg/pkg/tdglib"
	"golang.org/x/oauth2"
)

const (
	defaultMinWords      = 3
	defaultMinChars      = 30
	defaultLimit         = 0
	defaultIssuesPerPage = 100
	contextLinesUp       = 3
	contextLinesDown     = 7
	ghRoot               = "/github/workspace"
)

func sourceRoot(root string) string {
	if strings.HasPrefix(root, "/") {
		return ghRoot + root
	}

	return fmt.Sprintf("%s/%v", ghRoot, root)
}

type env struct {
	root      string
	owner     string
	repo      string
	label     string
	token     string
	sha       string
	includeRE string
	excludeRE string
	minWords  int
	minChars  int
	limit     int
	dryRun    bool
}

type service struct {
	ctx    context.Context
	client *github.Client
	env    *env
}

func (e *env) sourceRoot() string {
	return sourceRoot(e.root)
}

func environment() *env {
	e := &env{}
	r := strings.Split(os.Getenv("INPUT_REPO"), "/")
	e.owner, e.repo = r[0], r[1]
	e.label = os.Getenv("INPUT_LABEL")
	e.token = os.Getenv("INPUT_TOKEN")
	e.sha = os.Getenv("INPUT_SHA")
	e.includeRE = os.Getenv("INPUT_INCLUDE_PATTERN")
	e.excludeRE = os.Getenv("INPUT_EXCLUDE_PATTERN")
	e.root = os.Getenv("INPUT_ROOT")
	e.dryRun = len(os.Getenv("INPUT_DRY_RUN")) > 0

	var err error

	e.minWords, err = strconv.Atoi(os.Getenv("INPUT_MIN_WORDS"))
	if err != nil {
		e.minWords = defaultMinWords
	}

	e.minChars, err = strconv.Atoi(os.Getenv("INPUT_MIN_CHARACTERS"))
	if err != nil {
		e.minChars = defaultMinChars
	}

	e.limit, err = strconv.Atoi(os.Getenv("INPUT_LIMIT"))
	if err != nil {
		e.limit = defaultLimit
	}

	return e
}

func (s *service) fetchGithubIssues() ([]*github.Issue, error) {
	var allIssues []*github.Issue

	opt := &github.IssueListByRepoOptions{
		Labels:      []string{s.env.label},
		ListOptions: github.ListOptions{PerPage: defaultIssuesPerPage},
	}

	for {
		issues, resp, err := s.client.Issues.ListByRepo(s.ctx, s.env.owner, s.env.repo, opt)
		if err != nil {
			return nil, err
		}

		allIssues = append(allIssues, issues...)

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
	log.Printf("Fetched github issues. count=%v", len(allIssues))

	return allIssues, nil
}

func (s *service) createFileLink(c *tdglib.ToDoComment) string {
	start := c.Line - contextLinesUp
	if start < 0 {
		start = 0
	}

	end := c.Line + contextLinesDown

	root := strings.TrimPrefix(s.env.root, "/")
	root = strings.TrimSuffix(root, "/")

	filepath := c.File
	if root != "." {
		filepath = fmt.Sprintf("%v/%v", root, c.File)
	}

	// https://github.com/{repo}/blob/{sha}/{file}#L{startLines}-L{endLine}
	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%v-L%v",
		s.env.owner, s.env.repo, s.env.sha, filepath, start, end)
}

func (s *service) openNewIssues(issueMap map[string]*github.Issue, comments []*tdglib.ToDoComment) error {
	count := 0

	for _, c := range comments {
		_, ok := issueMap[c.Title]
		if !ok {
			body := c.Body + "\n\n"
			if c.Issue > 0 {
				body += fmt.Sprintf("Parent issue: #%v\n", c.Issue)
			}

			body += fmt.Sprintf("Line: %v\n%s", c.Line, s.createFileLink(c))

			log.Printf("About to create an issue. title=%v body=%v", c.Title, body)

			if s.env.dryRun {
				log.Printf("Dry run mode.")
				continue
			}

			labels := []string{s.env.label}

			issue := &github.IssueRequest{
				Title:  &c.Title,
				Body:   &body,
				Labels: &labels,
			}

			if _, _, err := s.client.Issues.Create(s.ctx, s.env.owner, s.env.repo, issue); err != nil {
				return err
			}

			count++
			if s.env.limit > 0 && count >= s.env.limit {
				log.Printf("Exceeded limit of issues to create. limit=%v", s.env.limit)
				break
			}
		}
	}

	return nil
}

func main() {
	log.SetOutput(os.Stdout)

	env := environment()
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: env.token},
	)
	tc := oauth2.NewClient(ctx, ts)

	svc := &service{
		ctx:    ctx,
		client: github.NewClient(tc),
		env:    env,
	}

	issues, err := svc.fetchGithubIssues()
	if err != nil {
		log.Panic(err)
	}

	includePatterns := make([]string, 0)
	if len(env.includeRE) > 0 {
		includePatterns = append(includePatterns, env.includeRE)
	}

	excludePatterns := make([]string, 0)
	if len(env.excludeRE) > 0 {
		excludePatterns = append(excludePatterns, env.excludeRE)
	}

	td := tdglib.NewToDoGenerator(env.sourceRoot(),
		includePatterns,
		excludePatterns,
		env.minWords,
		env.minChars)

	comments, err := td.Generate()
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Extracted TODO comments. count=%v", len(comments))

	issueMap := make(map[string]*github.Issue)
	for _, i := range issues {
		issueMap[i.GetTitle()] = i
	}

	err = svc.openNewIssues(issueMap, comments)
	if err != nil {
		log.Panic(err)
	}

	fmt.Println(fmt.Sprintf(`::set-output name=scannedIssues::%s`, "1"))
}
