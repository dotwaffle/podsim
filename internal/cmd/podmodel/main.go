// Command podmodel writes the pod model of the ride page, web/pod.glb. The
// model is generated, so a change to its shape is a change to this
// command. Run it from the repository root:
//
//	go run ./internal/cmd/podmodel
//
// TestModelFile fails when web/pod.glb does not match the output.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	output := flag.String("o", "web/pod.glb", "output path of the GLB file")
	flag.Parse()
	data, err := encodeGLB(buildModel())
	if err == nil {
		err = os.WriteFile(*output, data, 0o644) // #nosec G306 -- The model is a public web asset.
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "podmodel:", err)
		os.Exit(1)
	}
}
