package main

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/gocolly/colly/v2"
    "github.com/google/go-github/v57/github"
    "golang.org/x/oauth2"
)

// ProjectData represents a discovered project
type ProjectData struct {
    Name          string
    Description   string
    Stars         int
    Language      string
    LastCommit    time.Time
    Contributors  int
    Issues        int
    PullRequests  int
    License       string
    Topics        []string
}

// Crawler manages the distributed crawling process
type Crawler struct {
    client    *github.Client
    collector *colly.Collector
    workers   int
    queue     chan string
    results   chan ProjectData
    wg        sync.WaitGroup
}

// NewCrawler initializes a new crawler instance
func NewCrawler(token string, workers int) *Crawler {
    ctx := context.Background()
    ts := oauth2.StaticTokenSource(
        &oauth2.Token{AccessToken: token},
    )
    tc := oauth2.NewClient(ctx, ts)

    c := colly.NewCollector(
        colly.Async(true),
        colly.MaxDepth(2),
    )

    // Rate limiting: 5000 requests per hour
    c.Limit(&colly.LimitRule{
        DomainGlob:  "*github.com*",
        Parallelism: workers,
        Delay:       1 * time.Second,
    })

    return &Crawler{
        client:    github.NewClient(tc),
        collector: c,
        workers:   workers,
        queue:     make(chan string, workers*10),
        results:   make(chan ProjectData, workers*5),
    }
}

// Start begins the crawling process
func (cr *Crawler) Start(ctx context.Context, topics []string) {
    log.Printf("Starting crawler with %d workers
", cr.workers)

    // Start worker pool
    for i := 0; i < cr.workers; i++ {
        cr.wg.Add(1)
        go cr.worker(ctx, i)
    }

    // Seed the queue with initial repositories
    go func() {
        for _, topic := range topics {
            repos, err := cr.searchReposByTopic(ctx, topic)
            if err != nil {
                log.Printf("Error searching topic %s: %v
", topic, err)
                continue
            }

            for _, repo := range repos {
                cr.queue <- *repo.FullName
            }
        }
        close(cr.queue)
    }()

    // Wait for completion
    cr.wg.Wait()
    close(cr.results)
}

// worker processes repositories from the queue
func (cr *Crawler) worker(ctx context.Context, id int) {
    defer cr.wg.Done()

    for repoName := range cr.queue {
        log.Printf("Worker %d: Processing %s
", id, repoName)

        data, err := cr.fetchProjectData(ctx, repoName)
        if err != nil {
            log.Printf("Worker %d: Error fetching %s: %v
", id, repoName, err)
            continue
        }

        cr.results <- data
    }
}

// fetchProjectData retrieves detailed information about a repository
func (cr *Crawler) fetchProjectData(ctx context.Context, fullName string) (ProjectData, error) {
    // Parse owner/repo
    owner, repo := parseFullName(fullName)

    // Fetch repository details
    repoData, _, err := cr.client.Repositories.Get(ctx, owner, repo)
    if err != nil {
        return ProjectData{}, err
    }

    // Fetch contributors count
    contributors, err := cr.getContributorCount(ctx, owner, repo)
    if err != nil {
        log.Printf("Error fetching contributors for %s: %v
", fullName, err)
        contributors = 0
    }

    // Fetch issues count
    issues, err := cr.getOpenIssuesCount(ctx, owner, repo)
    if err != nil {
        log.Printf("Error fetching issues for %s: %v
", fullName, err)
        issues = 0
    }

    return ProjectData{
        Name:         *repoData.Name,
        Description:  repoData.GetDescription(),
        Stars:        repoData.GetStargazersCount(),
        Language:     repoData.GetLanguage(),
        LastCommit:   repoData.GetPushedAt().Time,
        Contributors: contributors,
        Issues:       issues,
        PullRequests: repoData.GetOpenIssuesCount() - issues,
        License:      repoData.GetLicense().GetName(),
        Topics:       repoData.Topics,
    }, nil
}

// searchReposByTopic searches GitHub for repositories by topic
func (cr *Crawler) searchReposByTopic(ctx context.Context, topic string) ([]*github.Repository, error) {
    query := fmt.Sprintf("topic:%s good-first-issue:>5 stars:>100", topic)

    opts := &github.SearchOptions{
        Sort:  "stars",
        Order: "desc",
        ListOptions: github.ListOptions{
            PerPage: 100,
        },
    }

    result, _, err := cr.client.Search.Repositories(ctx, query, opts)
    if err != nil {
        return nil, err
    }

    return result.Repositories, nil
}

// getContributorCount fetches the number of contributors
func (cr *Crawler) getContributorCount(ctx context.Context, owner, repo string) (int, error) {
    opts := &github.ListContributorsOptions{
        ListOptions: github.ListOptions{PerPage: 1},
    }

    _, resp, err := cr.client.Repositories.ListContributors(ctx, owner, repo, opts)
    if err != nil {
        return 0, err
    }

    return resp.LastPage, nil
}

// getOpenIssuesCount fetches the number of open issues
func (cr *Crawler) getOpenIssuesCount(ctx context.Context, owner, repo string) (int, error) {
    opts := &github.IssueListByRepoOptions{
        State: "open",
        ListOptions: github.ListOptions{PerPage: 1},
    }

    _, resp, err := cr.client.Issues.ListByRepo(ctx, owner, repo, opts)
    if err != nil {
        return 0, err
    }

    return resp.LastPage, nil
}

// Helper function to parse owner/repo from full name
func parseFullName(fullName string) (string, string) {
    // Simple parsing (in production, use proper URL parsing)
    // Example: "owner/repo" -> "owner", "repo"
    return "", "" // Placeholder
}

func main() {
    token := "your_github_token" // From environment variable
    topics := []string{"machine-learning", "web-development", "data-science", "devops"}

    crawler := NewCrawler(token, 50)
    ctx := context.Background()

    // Start crawler
    go crawler.Start(ctx, topics)

    // Process results
    processed := 0
    for data := range crawler.results {
        fmt.Printf("Discovered: %s (%d stars)\n", data.Name, data.Stars)
        processed++

        // In production: save to database, send to ML service for classification
    }

    log.Printf("Crawling complete. Processed %d repositories\n", processed)
}
