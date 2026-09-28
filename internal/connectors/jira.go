package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
)

type Jira struct {
	source spec.Source
	api    *apiClient
}

func NewJira(source spec.Source, email, token string, client *http.Client) (*Jira, error) {
	auth := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	api, err := newAPIClient(source.Jira.BaseURL, client, map[string]string{"Authorization": "Basic " + auth})
	if err != nil {
		return nil, err
	}
	return &Jira{source: source, api: api}, nil
}

func (jira *Jira) Test(ctx context.Context) error {
	var result struct {
		AccountID string `json:"accountId"`
	}
	_, err := jira.api.json(ctx, http.MethodGet, "/rest/api/3/myself", nil, nil, &result)
	if err != nil {
		return err
	}
	if result.AccountID == "" {
		return fmt.Errorf("Jira authentication response did not identify a user")
	}
	return nil
}

type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string  `json:"summary"`
		Updated apiTime `json:"updated"`
		Status  struct {
			Name string `json:"name"`
		} `json:"status"`
		Assignee *struct {
			AccountID   string `json:"accountId"`
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
	} `json:"fields"`
}

type jiraHistory struct {
	ID      string  `json:"id"`
	Created apiTime `json:"created"`
	Items   []struct {
		Field      string `json:"field"`
		FromString string `json:"fromString"`
		ToString   string `json:"toString"`
		To         string `json:"to"`
	} `json:"items"`
}

func (jira *Jira) Poll(ctx context.Context, cursor string, now time.Time) (Batch, error) {
	since := connectorSince(cursor, jira.source.Poll.InitialLookback, now)
	issues, reset, err := jira.search(ctx, since)
	if err != nil {
		return Batch{}, err
	}
	batch := Batch{Cursor: now.Add(-2 * time.Second).Format(time.RFC3339Nano), RateLimitResetAt: reset, Events: []Emission{}, WorkItems: []store.WorkItem{}, Outcomes: []store.Outcome{}}
	for _, issue := range issues {
		item := jiraWorkItem(jira.source.Jira.BaseURL, issue, now)
		batch.WorkItems = append(batch.WorkItems, item)
		histories, rateReset, err := jira.changelog(ctx, issue.Key)
		if err != nil {
			return Batch{}, err
		}
		if rateReset.After(batch.RateLimitResetAt) {
			batch.RateLimitResetAt = rateReset
		}
		assigned := false
		for _, history := range histories {
			created := history.Created.Time()
			if created.Before(since) {
				continue
			}
			for index, change := range history.Items {
				switch strings.ToLower(change.Field) {
				case "assignee":
					if change.To == "" && change.ToString == "" {
						continue
					}
					assigned = true
					data, _ := json.Marshal(map[string]any{"issue_key": issue.Key, "summary": issue.Fields.Summary, "assignee": change.ToString, "status": issue.Fields.Status.Name})
					raw, _ := json.Marshal(map[string]any{"issue": issue, "history": history})
					batch.Events = append(batch.Events, Emission{Envelope: router.Envelope{ID: fmt.Sprintf("%s:assignee:%s:%d", issue.Key, history.ID, index), Source: jira.source.Name, Type: "jira.issue.assigned", Subject: issue.Key, Time: created, Data: data, RawData: raw}, WorkItem: item})
				case "status":
					data, _ := json.Marshal(map[string]any{"issue_key": issue.Key, "from": change.FromString, "to": change.ToString})
					batch.Outcomes = append(batch.Outcomes, store.Outcome{ID: stableID("out", jira.source.Name, issue.Key, history.ID, fmt.Sprint(index)), Type: "jira.issue.transitioned", Subject: "jira:" + issue.Key, WorkItemID: item.ID, OccurredAt: created, Data: data})
				}
			}
		}
		if cursor == "" && !assigned && issue.Fields.Assignee != nil {
			data, _ := json.Marshal(map[string]any{"issue_key": issue.Key, "summary": issue.Fields.Summary, "assignee": issue.Fields.Assignee.DisplayName, "status": issue.Fields.Status.Name})
			raw, _ := json.Marshal(issue)
			updated := issue.Fields.Updated.Time()
			batch.Events = append(batch.Events, Emission{Envelope: router.Envelope{ID: issue.Key + ":assigned:" + updated.UTC().Format(time.RFC3339Nano), Source: jira.source.Name, Type: "jira.issue.assigned", Subject: issue.Key, Time: updated, Data: data, RawData: raw}, WorkItem: item})
		}
	}
	return batch, nil
}

func (jira *Jira) search(ctx context.Context, since time.Time) ([]jiraIssue, time.Time, error) {
	issues := []jiraIssue{}
	token := ""
	var reset time.Time
	for page := 0; page < 20; page++ {
		jql := fmt.Sprintf("(%s) AND updated >= \"%s\" ORDER BY updated ASC", jira.source.Jira.JQL, since.UTC().Format("2006-01-02 15:04"))
		body, _ := json.Marshal(map[string]any{"jql": jql, "fields": []string{"summary", "status", "assignee", "updated"}, "maxResults": 100, "nextPageToken": token})
		var response struct {
			Issues        []jiraIssue `json:"issues"`
			NextPageToken string      `json:"nextPageToken"`
			IsLast        bool        `json:"isLast"`
		}
		header, err := jira.api.json(ctx, http.MethodPost, "/rest/api/3/search/jql", nil, bytes.NewReader(body), &response)
		if err != nil {
			return nil, time.Time{}, err
		}
		if value := rateLimitReset(header, time.Now().UTC()); header.Get("Retry-After") != "" && value.After(reset) {
			reset = value
		}
		issues = append(issues, response.Issues...)
		if response.IsLast || response.NextPageToken == "" {
			return issues, reset, nil
		}
		token = response.NextPageToken
	}
	return nil, time.Time{}, fmt.Errorf("Jira search exceeded the 20-page safety limit")
}

func (jira *Jira) changelog(ctx context.Context, key string) ([]jiraHistory, time.Time, error) {
	values := []jiraHistory{}
	start := 0
	var reset time.Time
	for page := 0; page < 20; page++ {
		query := url.Values{"startAt": {fmt.Sprint(start)}, "maxResults": {"100"}}
		var response struct {
			Values     []jiraHistory `json:"values"`
			IsLast     bool          `json:"isLast"`
			StartAt    int           `json:"startAt"`
			MaxResults int           `json:"maxResults"`
			Total      int           `json:"total"`
		}
		header, err := jira.api.json(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"/changelog", query, nil, &response)
		if err != nil {
			return nil, time.Time{}, err
		}
		if value := rateLimitReset(header, time.Now().UTC()); header.Get("Retry-After") != "" && value.After(reset) {
			reset = value
		}
		values = append(values, response.Values...)
		if response.IsLast || len(response.Values) == 0 || response.StartAt+response.MaxResults >= response.Total {
			return values, reset, nil
		}
		start = response.StartAt + response.MaxResults
	}
	return nil, time.Time{}, fmt.Errorf("Jira changelog exceeded the 20-page safety limit")
}

func jiraWorkItem(base string, issue jiraIssue, now time.Time) store.WorkItem {
	data, _ := json.Marshal(map[string]any{"url": strings.TrimRight(base, "/") + "/browse/" + issue.Key})
	return store.WorkItem{ID: stableID("work", "jira", issue.Key), Kind: "jira", ExternalKey: issue.Key, Title: issue.Fields.Summary, State: issue.Fields.Status.Name, Data: data, CreatedAt: now, UpdatedAt: now}
}

func connectorSince(cursor, lookback string, now time.Time) time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
		return parsed.UTC()
	}
	duration := time.Hour
	if parsed, err := time.ParseDuration(lookback); err == nil && parsed > 0 {
		duration = parsed
	}
	return now.Add(-duration)
}

type apiTime time.Time

func (value *apiTime) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05-0700"} {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			*value = apiTime(parsed)
			return nil
		}
	}
	return fmt.Errorf("unsupported API timestamp")
}

func (value apiTime) Time() time.Time { return time.Time(value) }

func (value apiTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.Time().UTC().Format(time.RFC3339Nano))
}
