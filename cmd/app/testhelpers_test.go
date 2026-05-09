package main

import (
	"bytes"

	"github.com/spf13/cobra"
)

func executeCommand(root *cobra.Command, args ...string) (stdout, stderr string, err error) {
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer

	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()

	return outBuf.String(), errBuf.String(), err
}
