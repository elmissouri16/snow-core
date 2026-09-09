package javascript

import (
	_ "embed"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

//go:embed scaffold/snow.d.ts
var TypeScriptDeclarations string

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
	files := map[string]string{"snow-plugin.json": string(raw) + "\n", "main.js": starterScript, "snow.d.ts": TypeScriptDeclarations, "README.md": fmt.Sprintf("# %s\n\nRegister: `snow plugin add .`\n\nValidate: `snow plugin check %s`\n\nRun: `snow plugin run %s:hello -- world` or `/%s:hello world` in the TUI.\n\nRestart Snow after editing a loaded plugin.\n", id, id, id, id)}
	if typescript {
		files["main.ts"] = starterScript
		files["tsconfig.json"] = `{"compilerOptions":{"strict":true,"target":"ES2020","module":"ESNext","lib":["ES2020"],"noEmit":true},"files":["main.ts","snow.d.ts"]}`
		files["package.json"] = `{"private":true,"scripts":{"build":"esbuild main.ts --bundle --format=iife --platform=neutral --outfile=main.js","check":"tsc --noEmit"}}`
		files["README.md"] += "\nFor TypeScript, run `npm install --save-dev esbuild typescript`, edit main.ts, then `npm run check && npm run build`. Distribute main.js and snow-plugin.json. Node APIs are unavailable at runtime.\n"
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(path, name), []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}
