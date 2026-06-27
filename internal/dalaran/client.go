package dalaran

import (
	"crypto/tls"
	"fmt"
	"strings"

	"gps/internal/model"

	"github.com/go-resty/resty/v2"
)

// Client fetches silo/repo product-tree data from a dalaran instance.
// Module information from dalaran is intentionally NOT consumed — GPS
// synthesizes its own module set and dependency graph.
//
// dalaran's GET /api/v1/silos is a public, unauthenticated API, so no token
// is sent. (The x-yunxiao-token in dalaran's own config is used by dalaran as
// a client toward its upstream config source — unrelated to its public API.)
type Client struct {
	baseURL string // e.g. https://dalaran.internal.com
	client  *resty.Client
}

func NewClient(baseURL string) *Client {
	c := resty.New()
	c.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) // 内网自签名实例
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  c,
	}
}

// --- dalaran wire format ---

type response struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    []businessSilo `json:"data"`
}

type businessSilo struct {
	Code   string     `json:"code"`
	Name   string     `json:"name"`
	NameEn string     `json:"name_en"`
	Repos  []codeRepo `json:"repos"`
	// Modules intentionally omitted — dalaran module info is not used.
}

type codeRepo struct {
	URL       string `json:"url"`
	DevOpsOpt bool   `json:"devopsOpt"`
}

// FetchTree retrieves all silos and their repos, mapping them into GPS models.
// Repos in dalaran carry only a URL, so id/name/release_branch are derived.
func (c *Client) FetchTree() ([]model.Silo, []model.Repo, error) {
	var resp response
	r, err := c.client.R().
		SetResult(&resp).
		Get(c.baseURL + "/api/v1/silos")
	if err != nil {
		return nil, nil, fmt.Errorf("dalaran request: %w", err)
	}
	if r.IsError() {
		return nil, nil, fmt.Errorf("dalaran returned %d: %s", r.StatusCode(), r.String())
	}
	if resp.Code != 0 {
		return nil, nil, fmt.Errorf("dalaran error code %d: %s", resp.Code, resp.Message)
	}

	silos, repos := mapTree(resp.Data)
	return silos, repos, nil
}

// mapTree converts dalaran silos into GPS Silo/Repo models. Exposed package-internal
// for testing. siloID uses dalaran's code; repo IDs are sequential and stable
// within one fetch.
func mapTree(data []businessSilo) ([]model.Silo, []model.Repo) {
	var silos []model.Silo
	var repos []model.Repo
	repoIdx := 0

	for _, bs := range data {
		siloID := bs.Code
		name := bs.NameEn
		if name == "" {
			name = bs.Code
		}
		silos = append(silos, model.Silo{
			ID:   siloID,
			Name: name,
			Desc: bs.Name,
		})

		for _, cr := range bs.Repos {
			if cr.URL == "" || !cr.DevOpsOpt {
				continue // skip repos without a URL or not enabled for devops
			}
			repoIdx++
			repos = append(repos, model.Repo{
				ID:            fmt.Sprintf("repo-%04d", repoIdx),
				SiloID:        siloID,
				Name:          repoNameFromURL(cr.URL),
				URL:           cr.URL,
				ReleaseBranch: "main",
				JDK:           "21",
			})
		}
	}
	return silos, repos
}

// repoNameFromURL derives a repo name from a git URL's last path segment.
// e.g. ssh://git@host:9022/group/issuance.git -> issuance
func repoNameFromURL(url string) string {
	s := url
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, ".git")
	if s == "" {
		return url
	}
	return s
}
