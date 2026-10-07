package generator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const routerMetadataFile = ".fw.json"

type projectMetadata struct {
	Router string `json:"router"`
}

// ResolveRouter reads CLI-only project metadata or an explicit router choice.
// It never silently selects a router for an existing application.
func ResolveRouter(dir, requested string) (string, error) {
	if requested != "" {
		if err := validateRouter(requested); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, routerMetadataFile)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if requested == "" {
			return "", fmt.Errorf("no %s: select the application's HTTP router with --router chi or --router gin", routerMetadataFile)
		}
		return requested, nil
	}
	if err != nil {
		return "", fmt.Errorf("read router metadata: %w", err)
	}
	var metadata projectMetadata
	if err := json.Unmarshal(content, &metadata); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if err := validateRouter(metadata.Router); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if requested != "" && requested != metadata.Router {
		return "", fmt.Errorf("--router %s conflicts with %s router %s; migrate the application and its metadata together", requested, path, metadata.Router)
	}
	return metadata.Router, nil
}

func writeRouterMetadata(dir, router string) error {
	content, err := json.MarshalIndent(projectMetadata{Router: router}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode router metadata: %w", err)
	}
	return writeFileExclusive(filepath.Join(dir, routerMetadataFile), append(content, '\n'), 0o644)
}
