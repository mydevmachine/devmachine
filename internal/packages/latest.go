package packages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// latestReleaseAPIURL is where GitHub answers with the newest published
// release. A variable so a test can answer without the network.
var latestReleaseAPIURL = "https://api.github.com/repos/mydevmachine/packages/releases/latest"

// releaseNotesBase is where a packages release is described for a person.
const releaseNotesBase = "https://github.com/mydevmachine/packages/releases/tag/"

// NotesURL is the page that says what changed in a packages release.
func NotesURL(tag string) string { return releaseNotesBase + tag }

// Latest resolves the release tag of the newest published packages release.
//
// This is the one place that answers "what is latest": `setup` pins it for a
// new configuration, and `skills add` falls back to it when there is no
// configuration at all. Nothing else guesses a version on its own.
func Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub's API refuses a request with no User-Agent.
	req.Header.Set("User-Agent", "devmachine-cli")

	resp, err := fetchClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("finding the latest packages release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("finding the latest packages release: the server answered %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("reading the latest packages release: %w", err)
	}
	if payload.TagName == "" {
		return "", errors.New("the latest packages release has no tag name")
	}
	return payload.TagName, nil
}
