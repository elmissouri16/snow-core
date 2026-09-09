package main

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"github.com/spf13/cobra"
)

func pluginRunCmd() *cobra.Command {
	command := &cobra.Command{Use: "run <plugin:command> -- [input]", Short: "Run a JavaScript workflow command", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) (err error) {
		opts, err := buildOptions(cmd)
		if err != nil {
			return err
		}
		a, err := app.New(cmd.Context(), opts)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, a.Close()) }()
		previousAssistant := ""
		latest, hasLatest := a.Session.(session.LatestAssistantStore)
		if hasLatest {
			previous, _, _ := latest.LatestAssistantMessage()
			previousAssistant = previous.ID
		}
		result, err := a.RunPluginCommand(cmd.Context(), args[0], strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		if len(result.Content) == 0 && hasLatest {
			if message, ok, readErr := latest.LatestAssistantMessage(); readErr == nil && ok && message.ID != previousAssistant {
				for _, block := range message.Content {
					if block.Type == protocol.BlockText {
						result.Content = append(result.Content, block)
					}
				}
			}
		}
		if jsonRequested(cmd) {
			encodeErr := jsonv2.MarshalWrite(cmd.OutOrStdout(), struct {
				Content []protocol.ContentBlock `json:"content"`
				IsError bool                    `json:"is_error"`
			}{result.Content, result.IsError})
			if encodeErr != nil {
				return encodeErr
			}
			if result.IsError {
				return errors.New("plugin command reported an error")
			}
			return nil
		}
		for _, block := range result.Content {
			if block.Type == protocol.BlockText {
				fmt.Fprintln(cmd.OutOrStdout(), printableGoalBlockedReason(block.Text))
			}
		}
		if result.IsError {
			return errors.New("plugin command reported an error")
		}
		return nil
	}}
	command.Flags().Bool("json", false, "output JSON")
	return command
}
