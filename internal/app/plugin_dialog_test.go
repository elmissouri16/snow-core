package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPluginDialogsPublishClosedChoices(t *testing.T) {
	for _, tc := range []struct {
		operation, arguments, answer string
	}{
		{"confirm", `{title:"Continue?"}`, "No"},
		{"select", `{title:"Color",options:["Blue","Green"]}`, "Blue"},
		{"form", `{title:"Color",fields:[{name:"color",title:"Color",type:"enum",choices:["Blue","Green"]}]}`, "Blue"},
		{"form", `{title:"Enabled",fields:[{name:"enabled",title:"Enabled",type:"boolean"}]}`, "true"},
	} {
		t.Run(tc.operation+tc.answer, func(t *testing.T) {
			t.Setenv("SNOW_HOME", t.TempDir())
			cwd := t.TempDir()
			dir := writeV2Fixture(t, cwd, "dialog", fmt.Sprintf(`snow.registerCommand({name:"ask",description:"Ask",uses:["ui"],async run(_,ctx){return JSON.stringify(await ctx.ui.%s(%s));}});`, tc.operation, tc.arguments), []string{"commands", "ui"}, nil)
			a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{dir}, UserInputHandler: func(_ context.Context, req protocol.UserInputRequest) (protocol.UserInputResponse, error) {
				q := req.Questions[0]
				if !q.ChoicesOnly || len(q.Options) == 0 {
					t.Errorf("dialog offers an invalid custom answer: %+v", q)
				}
				if tc.operation == "confirm" && q.Options[0].Label != "No" {
					t.Error("confirmation defaults to Yes")
				}
				return protocol.UserInputResponse{RequestID: req.ID, Answers: []protocol.UserInputAnswer{{QuestionID: q.ID, Answer: tc.answer}}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			result, err := a.RunPluginCommand(t.Context(), "dialog:ask", "")
			if err != nil || result.IsError {
				t.Fatalf("dialog result=%+v err=%v", result, err)
			}
			if tc.operation == "confirm" && result.Content[0].Text != "false" {
				t.Fatal("No did not return false")
			}
		})
	}
}

func TestPluginInvalidDialogDoesNotPublishUnanswerableRequest(t *testing.T) {
	for _, invocation := range []string{
		`select({title:"Empty",options:[]})`,
		`select({title:"Empty label",options:[""]})`,
		`select({title:"Duplicate",options:["Blue","Blue"]})`,
		`select({title:"Whitespace",options:[" Blue "]})`,
		`form({title:"Empty enum",fields:[{name:"color",type:"enum",choices:[]}]})`,
	} {
		t.Run(invocation, func(t *testing.T) {
			t.Setenv("SNOW_HOME", t.TempDir())
			cwd := t.TempDir()
			dir := writeV2Fixture(t, cwd, "dialog", `snow.registerCommand({name:"ask",description:"Ask",uses:["ui"],async run(_,ctx){return await ctx.ui.`+invocation+`;}});`, []string{"commands", "ui"}, nil)
			a, err := New(t.Context(), Options{CWD: cwd, Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{dir}, UserInputHandler: func(_ context.Context, req protocol.UserInputRequest) (protocol.UserInputResponse, error) {
				t.Error("invalid dialog reached the user")
				return protocol.UserInputResponse{}, fmt.Errorf("cannot answer")
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if _, err := a.RunPluginCommand(t.Context(), "dialog:ask", ""); err == nil {
				t.Fatal("invalid dialog succeeded")
			}
		})
	}
}
