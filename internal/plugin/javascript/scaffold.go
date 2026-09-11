package javascript

import (
	_ "embed"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

//go:embed scaffold/snow.d.ts
var TypeScriptDeclarations string

//go:embed scaffold/fixtures.md
var fixtureGuide string

const starterScript = `/// <reference path="./snow.d.ts" />
snow.registerView({name:"status",title:"Plugin status",placement:"footer"});
snow.registerCommand({name:"hello",description:"Show a greeting",uses:["ui","storage"],async run(input,ctx){
 const visits=Number(await ctx.storage.get({key:"visits"})||0)+1;
 await ctx.storage.set({key:"visits",value:visits});
 const text="Hello "+(input||"Snow")+" · visit "+visits;
 await ctx.ui.update({name:"status",content:{type:"text",text,tone:"accent"}});
 return text;
}});
`

// Scaffold refuses an existing directory, including symlinks. No dependencies
// are installed and no script is executed by this operation.
func Scaffold(path, id string, typescript bool) error {
	if err := plugin.ValidateIdentifier("plugin", id); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0755); err != nil {
		return err
	}
	manifest := Manifest{ID: id, Name: id, Version: "0.1.0", APIVersion: 2, Entry: "main.js", Capabilities: []string{"commands", "ui", "storage"}}
	raw, err := jsonv2.Marshal(manifest)
	if err != nil {
		return err
	}
	files := map[string]string{"snow-plugin.json": string(raw) + "\n", "main.js": starterScript, "snow.d.ts": TypeScriptDeclarations, "tests/plugin.json": starterFixtures, "tests/README.md": fixtureGuide, "README.md": fmt.Sprintf("# %s\n\nRegister: `snow plugin add .`\n\nValidate: `snow plugin check %s`\n\nRun: `snow plugin run %s:hello -- world` or `/%s:hello world` in the TUI.\n\nAfter editing a loaded plugin, use `/plugins reload %s` while idle (or restart Snow).\n\nMock-only tests: `snow plugin test . --fixtures tests/plugin.json`. Tests use actual Goja with an explicit fake host; they do not validate real permission, session, or tool dispatch behavior.\n", id, id, id, id, id)}
	if typescript {
		files["src/main.ts"] = "export default function register(snow: SnowAPI): void {\n" + strings.TrimPrefix(starterScript, "/// <reference path=\"./snow.d.ts\" />\n") + "}\n"
		files["src/entry.ts"] = "import register from \"./main\";\nregister(snow);\n"
		files["tsconfig.json"] = `{"compilerOptions":{"strict":true,"target":"ES2020","module":"ESNext","moduleResolution":"Bundler","lib":["ES2020"],"noEmit":true},"include":["src/**/*.ts","snow.d.ts"]}`
		files["package.json"] = `{"private":true,"scripts":{"build":"esbuild src/entry.ts --bundle --format=iife --platform=neutral --outfile=main.js","check":"tsc --noEmit","test":"npm run check && npm run build && snow plugin test . --fixtures tests/plugin.json"}}`
		files["README.md"] += "\nFor TypeScript, explicitly run `npm install --save-dev esbuild typescript`, edit the synchronous default factory in `src/main.ts`, then `npm run check && npm run build`. `src/entry.ts` calls the factory with the existing Snow global. Run `npm test` to typecheck, bundle, and test using the installed Snow CLI. Distribute main.js and snow-plugin.json. Node APIs are unavailable at runtime; Snow never installs or builds dependencies on startup or reload.\n"
	}
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(path, name)), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(path, name), []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

const starterFixtures = `{
 "version": 1,
 "tests": [{"name":"greeting uses declared mock storage and UI","steps":[
  {"kind":"command","name":"hello","input":"world","calls":[
   {"operation":"storage.get","args":{"key":"visits"},"memory":true},
   {"operation":"storage.set","args":{"key":"visits","value":1},"memory":true},
   {"operation":"ui.update","args":{"name":"status","content":{"type":"text","text":"Hello world · visit 1","tone":"accent"}}}
  ],"expect":{"content":[{"type":"text","text":"Hello world · visit 1"}]}}
 ]}]
}
`
