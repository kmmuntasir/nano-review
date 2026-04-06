package api

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePayload(t *testing.T) {
	tests := []struct {
		name    string
		payload ReviewPayload
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid payload",
			payload: ReviewPayload{
				RepoURL:    "git@github.com:owner/repo.git",
				PRNumber:   42,
				BaseBranch: "main",
				HeadBranch: "feature/x",
			},
			wantErr: false,
		},
		{
			name:    "missing repo URL",
			payload: ReviewPayload{PRNumber: 42, BaseBranch: "main", HeadBranch: "feature/x"},
			wantErr: true,
			errMsg:  "repo_url is required",
		},
		{
			name:    "missing PR number",
			payload: ReviewPayload{RepoURL: "git@github.com:owner/repo.git", BaseBranch: "main", HeadBranch: "feature/x"},
			wantErr: true,
			errMsg:  "pr_number is required",
		},
		{
			name:    "missing base branch",
			payload: ReviewPayload{RepoURL: "git@github.com:owner/repo.git", PRNumber: 42, HeadBranch: "feature/x"},
			wantErr: true,
			errMsg:  "base_branch is required",
		},
		{
			name:    "missing head branch",
			payload: ReviewPayload{RepoURL: "git@github.com:owner/repo.git", PRNumber: 42, BaseBranch: "main"},
			wantErr: true,
			errMsg:  "head_branch is required",
		},
		{
			name:    "all fields missing",
			payload: ReviewPayload{},
			wantErr: true,
			errMsg:  "repo_url is required",
		},
		{
			name: "empty string for repo URL",
			payload: ReviewPayload{
				RepoURL: "", PRNumber: 42, BaseBranch: "main", HeadBranch: "feature/x",
			},
			wantErr: true,
			errMsg:  "repo_url is required",
		},
		{
			name: "empty string for base branch",
			payload: ReviewPayload{
				RepoURL: "git@github.com:owner/repo.git", PRNumber: 42, BaseBranch: "", HeadBranch: "feature/x",
			},
			wantErr: true,
			errMsg:  "base_branch is required",
		},
		{
			name: "zero PR number",
			payload: ReviewPayload{
				RepoURL: "git@github.com:owner/repo.git", PRNumber: 0, BaseBranch: "main", HeadBranch: "feature/x",
			},
			wantErr: true,
			errMsg:  "pr_number is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePayload(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidPayload) {
				t.Errorf("ValidatePayload() error = %v, want ErrInvalidPayload wrapped", err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidatePayload() error = %v, want message containing %q", err, tt.errMsg)
			}
		})
	}
}
