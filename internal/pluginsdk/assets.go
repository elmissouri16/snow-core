package pluginsdk

import "embed"

// sdkAssets contains the private, zero-dependency plugin SDK runtimes shipped
// inside the Snow binary. TestSDKAssetsMatchSources prevents these reviewed
// copies from drifting from the canonical language package sources.
//
//go:embed all:assets
var sdkAssets embed.FS
