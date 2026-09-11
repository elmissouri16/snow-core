package javascript

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureTestPackage(script string) *Package {
	p := fixture(script)
	p.Manifest.APIVersion = 2
	p.Manifest.Capabilities = []string{"commands", "storage", "ui", "hooks", "tools", "workflow", "tool_policy"}
	return p
}
func parseTestFixtures(t *testing.T, text string) FixtureSuite {
	t.Helper()
	suite, err := ReadFixtures(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return suite
}
func TestFixturesCommandsMemoryEventsHooksAndMetadata(t *testing.T) {
	p := fixtureTestPackage(`
 let count=0;
 snow.registerCommand({name:"count",description:"counter",uses:["storage","ui"],async run(input,ctx){count++;await ctx.storage.set({key:"n",value:count});return String(await ctx.storage.get({key:"n"}));}});
 snow.registerTool({name:"echo",description:"echo",parameters:{type:"object"},execute(args){return args.text;}});
 snow.registerHook("before_prompt",req=>({text:req.text+"!"}));
 snow.onReady(async(_,ctx)=>{await ctx.ui.notify({text:"ready"});});
 snow.on("turn_done",async(_,ctx)=>{await ctx.storage.set({key:"event",value:true});});
 `)
	suite := parseTestFixtures(t, `{"version":1,"tests":[{"name":"sequence","steps":[
 {"kind":"metadata","select":"/plugin/commands/0/name","expect":"count"},
 {"kind":"metadata","select":"/tools/0/name","expect":"echo"},
 {"kind":"ready","calls":[{"operation":"ui.notify","args":{"text":"ready"}}]},
 {"kind":"command","name":"count","calls":[{"operation":"storage.set","args":{"key":"n","value":1},"memory":true},{"operation":"storage.get","args":{"key":"n"},"memory":true}],"select":"/content/0/text","expect":"1"},
 {"kind":"command","name":"count","calls":[{"operation":"storage.set","args":{"key":"n","value":2},"memory":true},{"operation":"storage.get","args":{"key":"n"},"memory":true}],"select":"/content/0/text","expect":"2"},
 {"kind":"tool","name":"echo","args":{"text":"echoed"},"expect":{"content":[{"type":"text","text":"echoed"}]}},
 {"kind":"hook","request":{"phase":"before_prompt","text":"hello"},"expect":{"text":"hello!"}},
 {"kind":"event","event":{"type":"turn_done","payload":{}},"calls":[{"operation":"storage.set","args":{"key":"event","value":true},"memory":true}]}
 ]}]}`)
	report, err := RunFixtures(t.Context(), p, suite)
	if err != nil || report.Failed != 0 || report.Passed != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	// A fresh runtime and deep-copied memory on every execution.
	report, err = RunFixtures(t.Context(), p, suite)
	if err != nil || report.Failed != 0 {
		t.Fatalf("repeat=%+v %v", report, err)
	}
}
func TestFixturesFailUnexpectedCallsEvenWhenCaught(t *testing.T) {
	p := fixtureTestPackage(`snow.registerCommand({name:"try",description:"try",uses:["ui"],async run(_,ctx){try{await ctx.ui.notify({text:"hidden"})}catch(e){} return "caught"}});`)
	for _, calls := range []string{`[]`, `[{"operation":"ui.notify","args":{"text":"other"}}]`} {
		suite := parseTestFixtures(t, `{"version":1,"tests":[{"name":"catch","steps":[{"kind":"command","name":"try","calls":`+calls+`}]}]}`)
		report, err := RunFixtures(t.Context(), p, suite)
		if err != nil || report.Failed != 1 {
			t.Fatalf("report=%+v err=%v", report, err)
		}
	}
}
func TestFixturesExpectedErrorsAndCapabilityBoundary(t *testing.T) {
	p := fixtureTestPackage(`snow.registerCommand({name:"allowed",description:"allowed",uses:["ui"],async run(_,ctx){await ctx.ui.notify({text:"test"})}});snow.registerCommand({name:"denied",description:"denied",async run(_,ctx){await ctx.ui.notify({text:"test"})}});snow.registerHook("before_tool",async(_,ctx)=>{await ctx.ui.notify({text:"test"});return {}});`)
	suite := parseTestFixtures(t, `{"version":1,"tests":[{"name":"errors","steps":[
 {"kind":"command","name":"allowed","calls":[{"operation":"ui.notify","args":{"text":"test"},"error":"mock unavailable"}],"error":"mock unavailable"},
 {"kind":"command","name":"denied","error":"undeclared capability"},
 {"kind":"hook","request":{"phase":"before_tool"},"error":"cannot perform host operations"}
 ]}]}`)
	report, err := RunFixtures(t.Context(), p, suite)
	if err != nil || report.Failed != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
func TestFixturesValidateInputAndCancellation(t *testing.T) {
	for _, text := range []string{`{}`, `{"version":2,"tests":[]}`, `{"version":1,"tests":[],"unknown":1}`, strings.Repeat(" ", MaxFixtureBytes+1), `{"version":1,"tests":[{"name":"x","steps":[{"kind":"bad"}]}]}`, `{"version":1,"tests":[{"name":"x","steps":[{"kind":"ready","expect":null,"error":"bad"}]}]}`} {
		if _, err := ReadFixtures(strings.NewReader(text)); err == nil {
			t.Fatalf("accepted %q", text[:min(len(text), 100)])
		}
	}
	suite := parseTestFixtures(t, `{"version":1,"tests":[{"name":"cancel","steps":[{"kind":"ready"}]}]}`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := RunFixtures(ctx, fixtureTestPackage(``), suite); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	report, err := RunFixtures(ctx, fixtureTestPackage(`while(true){}`), suite)
	if report.Failed != 1 && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout=%+v %v", report, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("runtime timeout was not bounded")
	}
}
func TestScaffoldFixturesAndTypeScriptLayout(t *testing.T) {
	for _, ts := range []bool{false, true} {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "demo")
		if err := Scaffold(path, "demo", ts); err != nil {
			t.Fatal(err)
		}
		p, err := ReadPackage(t.Context(), path, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(path, "tests/plugin.json"))
		if err != nil {
			t.Fatal(err)
		}
		suite, err := ReadFixtures(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		report, err := RunFixtures(t.Context(), p, suite)
		if err != nil || report.Failed != 0 {
			t.Fatalf("scaffold=%+v %v", report, err)
		}
		declarations, err := os.ReadFile(filepath.Join(path, "snow.d.ts"))
		if err != nil || string(declarations) != TypeScriptDeclarations {
			t.Fatal("declaration drift")
		}
		if ts {
			for _, name := range []string{"src/main.ts", "src/entry.ts", "tsconfig.json", "package.json"} {
				raw, err := os.ReadFile(filepath.Join(path, name))
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(name, ".json") {
					var value any
					if err := jsonv2.Unmarshal(raw, &value); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := os.Stat(filepath.Join(path, "node_modules")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("scaffold installed dependencies")
			}
		}
	}
}

func TestReferenceWorkflowFixturesAndDeclarationParity(t *testing.T) {
	shared, err := os.ReadFile("../../../examples/plugins/snow-v2.d.ts")
	if err != nil || string(shared) != TypeScriptDeclarations {
		t.Fatal("shared example declarations drifted from canonical API")
	}
	for _, name := range []string{"agent-profiles", "workflow-guard"} {
		t.Run(name, func(t *testing.T) {
			path, err := filepath.Abs(filepath.Join("../../../examples/plugins", name))
			if err != nil {
				t.Fatal(err)
			}
			p, err := ReadPackage(t.Context(), path, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(path, "snow.d.ts"))
			if err != nil || string(data) != TypeScriptDeclarations {
				t.Fatal("example declarations drifted from canonical API")
			}
			data, err = os.ReadFile(filepath.Join(path, "tests/plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			suite, err := ReadFixtures(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			report, err := RunFixtures(t.Context(), p, suite)
			if err != nil || report.Failed != 0 {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}

func TestFixturesAPI1ToolsAndSynchronousObservers(t *testing.T) {
	p := fixture(`let count=0;snow.on("turn_done",()=>{count++});snow.registerTool({name:"read-count",description:"read",parameters:{type:"object"},uses:["read"],execute(args,ctx){let result=ctx.callTool("read",{path:"test.txt"});return {content:[{type:"text",text:result.content[0].text+":"+count}]}}});`)
	suite := parseTestFixtures(t, `{"version":1,"tests":[{"name":"api1","steps":[{"kind":"event","event":{"type":"turn_done","payload":{}}},{"kind":"tool","name":"read-count","calls":[{"operation":"tools.call","args":{"name":"read","arguments":{"path":"test.txt"}},"result":{"content":[{"type":"text","text":"mocked"}]}}],"select":"/content/0/text","expect":"mocked:1"}]}]}`)
	report, err := RunFixtures(t.Context(), p, suite)
	if err != nil || report.Failed != 0 {
		t.Fatalf("API1 report=%+v err=%v", report, err)
	}
}
func TestFixturesWorkflowReceiptAndCapabilityValidation(t *testing.T) {
	p := fixtureTestPackage(`snow.registerCommand({name:"write",description:"write",uses:["workflow"],async run(_,ctx){const receipt=await ctx.workflow.set({key:"x",value:7});return receipt.branchId+":"+receipt.tipId}});snow.registerCommand({name:"invalid",description:"invalid",uses:["workflow"],async run(_,ctx){await ctx.workflow.update({toolRestriction:null})}});`)
	suite := parseTestFixtures(t, `{"version":1,"tests":[{"name":"receipt","steps":[{"kind":"command","name":"write","calls":[{"operation":"workflow.set","args":{"key":"x","value":7},"memory":true}],"select":"/content/0/text","expect":"fixture:fixture-1"}]},{"name":"missing tool policy declaration","steps":[{"kind":"command","name":"invalid","calls":[{"operation":"workflow.update","args":{"toolRestriction":null},"memory":true}],"error":"tool_policy"}]}]}`)
	report, err := RunFixtures(t.Context(), p, suite)
	if err != nil || report.Passed != 1 || report.Failed != 1 || !strings.Contains(report.Tests[1].Error, "tool_policy") {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestFixtureStorageDefaultScopeMatchesHost(t *testing.T) {
	p := fixtureTestPackage(`snow.registerCommand({name:"get",description:"get",uses:["storage"],async run(_,ctx){return String(await ctx.storage.get({key:"n"}))}});`)
	suite := parseTestFixtures(t, `{"version":1,"tests":[{"name":"project default","storage":{"project":{"n":4},"session":{"n":9}},"steps":[{"kind":"command","name":"get","calls":[{"operation":"storage.get","args":{"key":"n"},"memory":true}],"select":"/content/0/text","expect":"4"}]}]}`)
	report, err := RunFixtures(t.Context(), p, suite)
	if err != nil || report.Failed != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
