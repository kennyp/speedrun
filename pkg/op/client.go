package op

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Client is a wrapper around the `op` CLI tool
type Client struct {
	Account string // account shorthand, sign-in address, account ID, or user ID
}

// New returns a new 1Password Client
func New(account string) *Client {
	return &Client{
		Account: account,
	}
}

// SignIn ensures the user is signed in to 1Password
func (c *Client) SignIn(ctx context.Context) error {
	args := c.defaultArgs()
	args = append(args, "signin")

	cmd := exec.CommandContext(ctx, "op", args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		// Check if already signed in
		if strings.Contains(errBuf.String(), "already signed in") {
			return nil
		}
		return fmt.Errorf("trouble logging in: %s (%w)", errBuf.String(), err)
	}

	return nil
}

// Inject resolves op:// references in a template string
func (c *Client) Inject(ctx context.Context, template string) (string, error) {
	if !strings.HasPrefix(template, "op://") {
		return template, nil
	}

	args := c.defaultArgs()
	args = append(args, "inject")

	cmd := exec.CommandContext(ctx, "op", args...)
	cmd.Stdin = strings.NewReader(template)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to inject op reference: %s (%w)", errBuf.String(), err)
	}

	return outBuf.String(), nil
}

// Available checks if the op command is available
func (c *Client) Available() bool {
	cmd := exec.Command("op", "--version")
	return cmd.Run() == nil
}

func (c *Client) defaultArgs() []string {
	args := []string{"--format=json"}

	if c.Account != "" {
		args = append(args, "--account="+c.Account)
	}

	return args
}