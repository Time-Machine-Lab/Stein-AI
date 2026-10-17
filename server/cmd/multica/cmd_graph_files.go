package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/pkg/graphfiles"
	"github.com/spf13/cobra"
)

type graphFileInput struct {
	ID           string `json:"id"`
	Filename     string `json:"filename"`
	MediaType    string `json:"media_type"`
	Size         int64  `json:"size"`
	ContentHash  string `json:"content_hash,omitempty"`
	BundleFormat string `json:"bundle_format,omitempty"`
}
type graphFileContext struct {
	Manifest     graph.AttemptManifest `json:"manifest"`
	RootSnapshot struct {
		Attachments []graphFileInput `json:"attachments"`
	} `json:"root_snapshot"`
}

func loadGraphFileContext(ctx context.Context, client *cli.APIClient) (graphFileContext, error) {
	var current graphFileContext
	if err := client.GetJSON(ctx, "/api/graph/node/context", &current); err != nil {
		return current, err
	}
	if err := graph.ValidateIdentity(current.Manifest.Identity, current.Manifest.Identity); err != nil {
		return current, err
	}
	if client.TaskID != "" && current.Manifest.Identity.TaskID != client.TaskID {
		return current, fmt.Errorf("Graph context belongs to a different task")
	}
	return current, nil
}

func runGraphNodeDeliver(cmd *cobra.Command, args []string) error {
	within, err := fileWithinWorkingDir(args[0])
	if err != nil {
		return err
	}
	if !within {
		return fmt.Errorf("delivery directory must be inside the current task working directory")
	}
	client, err := graphCLIClient(cmd, true)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), cli.AtLeastAPITimeout(2*time.Minute))
	defer cancel()
	artifact, err := deliverGraphFiles(ctx, client, args[0])
	if err != nil {
		return err
	}
	return cli.PrintJSON(cmd.OutOrStdout(), map[string]any{"artifact": artifact, "instruction": "Copy artifact into result.artifacts before submitting; text and local paths alone do not deliver files."})
}

func deliverGraphFiles(ctx context.Context, client *cli.APIClient, source string) (graph.Artifact, error) {
	var artifact graph.Artifact
	current, err := loadGraphFileContext(ctx, client)
	if err != nil {
		return artifact, err
	}
	data, err := graphfiles.Build(source)
	if err != nil {
		return artifact, err
	}
	if err = graphfiles.Validate(data); err != nil {
		return artifact, err
	}
	att, err := client.UploadChatAttachment(ctx, data, "graph-files.zip", current.Manifest.Identity.TaskID)
	if err != nil {
		return artifact, err
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	if _, err = uuid.Parse(att.ID); err != nil {
		return artifact, fmt.Errorf("upload returned an invalid attachment identity")
	}
	if att.ContentHash != hash || att.SizeBytes != int64(len(data)) || att.ContentType != "application/zip" {
		return artifact, fmt.Errorf("file upload receipt does not match the delivered bundle")
	}
	return graph.Artifact{ID: att.ID, Type: graph.ArtifactAttachmentRef, ProducerTaskID: current.Manifest.Identity.TaskID, ContentHash: hash, Size: att.SizeBytes, MediaType: att.ContentType, BundleFormat: graphfiles.Format}, nil
}

type graphDownloadedInput struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Bundle bool   `json:"bundle"`
}

func graphInputFilename(name, id string) string {
	unsafe := name == "" || name == "." || name == ".." || strings.ContainsAny(name, "\\/:<>\"|?*") || strings.TrimRight(name, ". ") != name
	for _, ch := range name {
		unsafe = unsafe || ch < 32 || ch == 127
	}
	base := strings.ToLower(strings.SplitN(name, ".", 2)[0])
	unsafe = unsafe || base == "con" || base == "prn" || base == "aux" || base == "nul" || len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9'
	if unsafe {
		return "attachment-" + id
	}
	return name
}

func runGraphNodeDownloadInputs(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output-dir")
	within, err := fileWithinWorkingDir(output)
	if err != nil {
		return err
	}
	if !within {
		return fmt.Errorf("input directory must be inside the current task working directory")
	}
	client, err := graphCLIClient(cmd, true)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), cli.AtLeastAPITimeout(2*time.Minute))
	defer cancel()
	inputs, err := downloadGraphInputs(ctx, client, output)
	if err != nil {
		return err
	}
	return cli.PrintJSON(cmd.OutOrStdout(), map[string]any{"inputs": inputs, "instruction": "Inspect input images and start your own preview from the downloaded implementation. Another task's local path or localhost URL is not a delivered environment."})
}

func downloadGraphInputs(ctx context.Context, client *cli.APIClient, output string) ([]graphDownloadedInput, error) {
	current, err := loadGraphFileContext(ctx, client)
	if err != nil {
		return nil, err
	}
	inputs := append([]graphFileInput(nil), current.RootSnapshot.Attachments...)
	for _, artifact := range current.Manifest.NodeSpec.InputArtifacts {
		if artifact.Type == graph.ArtifactAttachmentRef {
			inputs = append(inputs, graphFileInput{ID: artifact.ID, MediaType: artifact.MediaType, Size: artifact.Size, ContentHash: artifact.ContentHash, BundleFormat: artifact.BundleFormat})
		}
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	if _, err = os.Lstat(output); err == nil {
		return nil, fmt.Errorf("input directory already exists; choose a new directory to avoid mixing attempts")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err = os.MkdirAll(output, 0700); err != nil {
		return nil, err
	}
	results := []graphDownloadedInput{}
	seen := map[string]bool{}
	for _, input := range inputs {
		if seen[input.ID] {
			continue
		}
		seen[input.ID] = true
		if _, err = uuid.Parse(input.ID); err != nil {
			return nil, fmt.Errorf("invalid input attachment identity")
		}
		var att cli.AttachmentResponse
		if err = client.GetJSON(ctx, "/api/attachments/"+input.ID, &att); err != nil {
			return nil, fmt.Errorf("input %s: %w", input.ID, err)
		}
		if att.ID != input.ID || att.SizeBytes != input.Size || att.ContentType != input.MediaType || att.DownloadURL == "" {
			return nil, fmt.Errorf("input %s metadata differs from the frozen input", input.ID)
		}
		data, err := client.DownloadFile(ctx, att.DownloadURL)
		if err != nil {
			return nil, fmt.Errorf("download input %s: %w", input.ID, err)
		}
		if int64(len(data)) != input.Size {
			return nil, fmt.Errorf("input %s size mismatch", input.ID)
		}
		if input.ContentHash != "" {
			digest := sha256.Sum256(data)
			if hex.EncodeToString(digest[:]) != input.ContentHash {
				return nil, fmt.Errorf("input %s hash mismatch", input.ID)
			}
		}
		dest := filepath.Join(output, input.ID)
		bundle := input.BundleFormat == graphfiles.Format
		if bundle {
			if err = graphfiles.Extract(data, dest); err != nil {
				return nil, fmt.Errorf("input %s: %w", input.ID, err)
			}
		} else {
			name := graphInputFilename(att.Filename, input.ID)
			if err = os.Mkdir(dest, 0700); err != nil {
				return nil, err
			}
			dest = filepath.Join(dest, name)
			if err = os.WriteFile(dest, data, 0600); err != nil {
				return nil, err
			}
		}
		results = append(results, graphDownloadedInput{ID: input.ID, Path: dest, Bundle: bundle})
	}
	return results, nil
}
