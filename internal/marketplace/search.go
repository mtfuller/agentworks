package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mtfuller/agentworks/internal/importer"
)

// httpClient and agentSkillsBaseURL are package vars so tests can point
// them at an httptest.Server instead of the real internet.
var (
	httpClient         = &http.Client{Timeout: 20 * time.Second}
	agentSkillsBaseURL = "https://agentskills.codes"
)

// maxManifestBytes caps how much of a marketplace.json or an
// agentskills.codes response body gets decoded.
const maxManifestBytes = 8 << 20

// Result is one search hit, already resolved to something
// importer.Prepare/ImportFromSource can act on directly.
type Result struct {
	Origin      string // "agentskills.codes", or a Marketplace.Name
	Name        string
	Description string
	Source      importer.Source

	// Optional details, filled from the marketplace entry where it has them.
	Author   string
	Category string
	Version  string
	Homepage string
	Keywords []string
	// License is the SPDX id the plugin was verified to carry, or "" if it
	// wasn't looked up (Options.CommercialOnly unset) or couldn't be found.
	License string
}

// Options tunes a Search.
type Options struct {
	// CommercialOnly keeps only plugins whose license is verifiably
	// permissive (see IsCommerciallySafe) -- either declared in the
	// marketplace entry or read from the repo's LICENSE file. Anything
	// whose license is missing or unrecognized is dropped, and
	// agentskills.codes is skipped entirely: its API exposes neither a
	// license nor a source repo to check one against.
	CommercialOnly bool
}

// Search queries agentskills.codes (server-side, via its q parameter) and
// every WellKnown marketplace (client-side substring filter over its
// already-fetched plugin list) for query. It returns whatever succeeded
// plus one error per source that didn't -- a dead source (an API 500, a
// 404'd marketplace.json) must never blank the whole pane when the others
// are fine.
func Search(ctx context.Context, query string, opts Options) ([]Result, []error) {
	var results []Result
	var errs []error

	if !opts.CommercialOnly {
		skills, err := searchAgentSkills(ctx, query)
		if err != nil {
			errs = append(errs, fmt.Errorf("agentskills.codes: %w", err))
		} else {
			results = append(results, skills...)
		}
	}

	for _, m := range WellKnown {
		found, err := searchMarketplace(ctx, m, query)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Name, err))
			continue
		}
		if opts.CommercialOnly {
			found = keepCommerciallySafe(ctx, found)
		}
		results = append(results, found...)
	}

	return results, errs
}

// licenseWorkers bounds concurrent LICENSE lookups -- a full marketplace is
// a few hundred plugins across many repos.
const licenseWorkers = 12

// keepCommerciallySafe resolves each result's license (a declared one wins;
// otherwise it's read from the repo) and drops everything that isn't
// permissive. Survivors get License set to the id that qualified them.
func keepCommerciallySafe(ctx context.Context, in []Result) []Result {
	licenses := make([]string, len(in))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < licenseWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if IsCommerciallySafe(in[i].License) {
					licenses[i] = in[i].License
				} else if in[i].License == "" {
					licenses[i] = lookupLicense(ctx, in[i].Source)
				}
			}
		}()
	}
	for i := range in {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	out := in[:0:0]
	for i, r := range in {
		if IsCommerciallySafe(licenses[i]) {
			r.License = licenses[i]
			out = append(out, r)
		}
	}
	return out
}

type agentSkillsResponse struct {
	Skills []struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Author      string `json:"author"`
		Description string `json:"description"`
	} `json:"skills"`
}

func searchAgentSkills(ctx context.Context, query string) ([]Result, error) {
	u := agentSkillsBaseURL + "/api/v1/skills?limit=50"
	if query != "" {
		u += "&q=" + url.QueryEscape(query)
	} else {
		u += "&sort=popular"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response %s", resp.Status)
	}

	var doc agentSkillsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxManifestBytes)).Decode(&doc); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(doc.Skills))
	for _, s := range doc.Skills {
		name := s.Name
		if s.Author != "" {
			name = fmt.Sprintf("%s (%s)", s.Name, s.Author)
		}
		results = append(results, Result{
			Origin:      "agentskills.codes",
			Name:        name,
			Description: s.Description,
			Source: importer.Source{
				Kind: importer.SourceArchive,
				URL:  fmt.Sprintf("%s/api/skills/download/%d", agentSkillsBaseURL, s.ID),
			},
		})
	}
	return results, nil
}

func fetchManifest(ctx context.Context, m Marketplace) (*manifestDoc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.ManifestURL(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response %s", resp.Status)
	}

	var doc manifestDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxManifestBytes)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parsing marketplace.json: %w", err)
	}
	return &doc, nil
}

func searchMarketplace(ctx context.Context, m Marketplace, query string) ([]Result, error) {
	doc, err := fetchManifest(ctx, m)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)
	var results []Result
	for _, e := range doc.Plugins {
		if q != "" && !strings.Contains(strings.ToLower(e.Name), q) && !strings.Contains(strings.ToLower(e.Description), q) {
			continue
		}
		src, err := e.resolve(m)
		if err != nil {
			continue // this entry's source type isn't supported yet -- skip it, don't fail the whole marketplace
		}
		results = append(results, Result{
			Origin:      m.Name,
			Name:        e.displayName(),
			Description: e.Description,
			Source:      src,
			Author:      string(e.Author),
			Category:    e.Category,
			Version:     e.Version,
			Homepage:    e.Homepage,
			Keywords:    append(append([]string(nil), e.Keywords...), e.Tags...),
			License:     string(e.License),
		})
	}
	return results, nil
}
