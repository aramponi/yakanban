// Command gendemo condenses a recorded take of the hero clip into the
// timeline the page replays.
//
//	site/demo/record.sh site/demo/takes/take-3
//	go run ./cmd/gendemo -take site/demo/takes/take-3
//
// The take stays local; the timeline is what gets committed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aramponi/yakanban/internal/site/demo"
)

func main() {
	var (
		take = flag.String("take", "", "take directory written by site/demo/record.sh")
		out  = flag.String("out", "site/demo/session.json", "timeline to write")
	)
	flag.Parse()
	if *take == "" {
		fmt.Fprintln(os.Stderr, "gendemo: -take is required")
		os.Exit(2)
	}
	if err := run(*take, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gendemo:", err)
		os.Exit(1)
	}
}

func run(take, out string) error {
	t, err := demo.Load(take)
	if err != nil {
		return err
	}
	tl, err := demo.Condense(t)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(tl, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d events, %s of session shown in %s\n", out, len(tl.Events),
		time.Duration(tl.Real)*time.Millisecond/time.Second*time.Second,
		time.Duration(tl.Duration)*time.Millisecond/time.Second*time.Second)
	return nil
}
