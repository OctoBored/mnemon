package access

// capsuleclient.go — the node's hub-v1 capsule client (r4-hub-protocol-v1):
// same bounded transport, bearer token, and T2 gate as the sync client;
// only the wire shape is capsule-native. problem+json decodes into a local
// type so access never imports the hub package (trust-domain boundary).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// HubProblem mirrors the hub's problem+json body (§5 closed type set).
type HubProblem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func (p *HubProblem) Error() string {
	return fmt.Sprintf("%s: %s", p.Type, p.Detail)
}

type CapsuleAccepted struct {
	CapsuleID string `json:"capsule_id"`
	HubSeq    int64  `json:"hub_seq"`
}

// CapsulePush POSTs one DSSE envelope. Returns replayed=true on the
// idempotent 200 path; a 422/403 rejection returns the decoded problem.
func (c *Client) CapsulePush(raw []byte) (CapsuleAccepted, bool, *HubProblem, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/capsules", bytes.NewReader(raw))
	if err != nil {
		return CapsuleAccepted{}, false, nil, err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/vnd.dsse+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return CapsuleAccepted{}, false, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		var out struct {
			Accepted CapsuleAccepted `json:"accepted"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return CapsuleAccepted{}, false, nil, fmt.Errorf("capsule push response: %w", err)
		}
		return out.Accepted, resp.Header.Get("Idempotency-Replayed") == "true", nil, nil
	default:
		var problem HubProblem
		if err := json.Unmarshal(body, &problem); err != nil || problem.Type == "" {
			return CapsuleAccepted{}, false, nil, fmt.Errorf("capsule push failed: %s: %s", resp.Status, string(body))
		}
		return CapsuleAccepted{}, false, &problem, nil
	}
}

type CapsuleFeedPage struct {
	Items       [][]byte
	NextCursor  int64
	ETag        string
	NotModified bool
}

// CapsulePull GETs accepted capsules after cursor; a matching etag returns
// NotModified with an empty page (the zero-cost idle pass).
func (c *Client) CapsulePull(cursor int64, limit int, etag string) (CapsuleFeedPage, error) {
	url := c.baseURL + "/capsules?cursor=" + strconv.FormatInt(cursor, 10)
	if limit > 0 {
		url += "&limit=" + strconv.Itoa(limit)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return CapsuleFeedPage{}, err
	}
	c.setAuth(req)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return CapsuleFeedPage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return CapsuleFeedPage{NextCursor: cursor, ETag: etag, NotModified: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return CapsuleFeedPage{}, fmt.Errorf("capsule pull failed: %s: %s", resp.Status, string(body))
	}
	var out struct {
		Items      []json.RawMessage `json:"items"`
		NextCursor int64             `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CapsuleFeedPage{}, err
	}
	page := CapsuleFeedPage{NextCursor: out.NextCursor, ETag: resp.Header.Get("ETag")}
	for _, item := range out.Items {
		page.Items = append(page.Items, []byte(item))
	}
	return page, nil
}

// BlobPut uploads one content-addressed blob (idempotent: 204 = already there).
func (c *Client) BlobPut(digest string, data []byte) error {
	req, err := http.NewRequest(http.MethodPut, c.baseURL+"/blobs/"+digest, bytes.NewReader(data))
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("blob put %s failed: %s: %s", digest, resp.Status, string(body))
	}
	return nil
}

// BlobGet fetches one blob from the hub lane.
func (c *Client) BlobGet(digest string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/blobs/"+digest, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("blob get %s failed: %s", digest, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
