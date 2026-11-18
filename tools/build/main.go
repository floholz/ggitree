// Command build bundles web/src into web/dist with esbuild's Go API.
//
// Run from the web directory (go generate does this): go run ../tools/build [-watch]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/evanw/esbuild/pkg/api"
)

func main() {
	watch := flag.Bool("watch", false, "rebuild on changes")
	flag.Parse()

	opts := api.BuildOptions{
		EntryPoints:       []string{"src/main.ts"},
		Outdir:            "dist",
		Bundle:            true,
		Write:             true,
		Format:            api.FormatESModule,
		Target:            api.ES2022,
		MinifyWhitespace:  !*watch,
		MinifySyntax:      !*watch,
		MinifyIdentifiers: !*watch,
		Sourcemap:         api.SourceMapLinked,
		LogLevel:          api.LogLevelInfo,
		Loader:            map[string]api.Loader{".css": api.LoaderCSS, ".svg": api.LoaderText},
	}
	if !*watch {
		if res := api.Build(opts); len(res.Errors) > 0 {
			os.Exit(1)
		}
		return
	}
	ctx, err := api.Context(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := ctx.Watch(api.WatchOptions{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("watching src/ for changes")
	select {}
}
