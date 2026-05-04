package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/fookiejs/fookie/pkg/schema"
	"github.com/fookiejs/fookie-simulator/pkg/simulator"
)

func main() {
	schemaPath := flag.String("schema", "", "path to main.fql or directory of .fql")
	url := flag.String("url", "http://127.0.0.1:8080/graphql", "GraphQL HTTP endpoint")
	n := flag.Int("n", 20, "number of random steps")
	seed := flag.Int64("seed", 1, "rng seed")
	validRatio := flag.Float64("valid", 0.65, "probability [0-1] of schema-shaped payloads")
	token := flag.String("token", "", "optional Bearer token (Authorization)")
	mergeRooms := flag.Bool("merge-rooms", false, "merge builtin room schema (demo WS subscriptions)")
	verbose := flag.Bool("v", false, "log every step")
	flag.Parse()

	if *schemaPath == "" {
		fmt.Fprintln(os.Stderr, "usage: simulator -schema <path> [-url ...] [-n 20]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	sch, err := schema.LoadSchema(*schemaPath)
	if err != nil {
		log.Fatal(err)
	}
	if *mergeRooms {
		if err := schema.MergeBuiltinRooms(sch); err != nil {
			log.Fatal(err)
		}
	}

	if simulator.SchemaProductionMode(sch) {
		fmt.Fprintln(os.Stderr, "simulator: schema config production=true; skipping GraphQL traffic")
		if os.Getenv("SIMULATOR_BLOCK_ON_PRODUCTION") == "1" {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
			<-ch
		}
		os.Exit(0)
	}

	r := simulator.NewRunner(sch, *url, *seed, *validRatio)
	r.AuthBearer = *token

	ok := 0
	fail := 0
	skip := 0
	for i := 0; i < *n; i++ {
		label, success, detail := r.Step()
		if detail == "skip-no-id" || detail == "skip" || detail == "no-op" {
			skip++
			if *verbose {
				fmt.Printf("[%d] %s — skip (%s)\n", i+1, label, detail)
			}
			continue
		}
		if label == "" && detail == "no-models" {
			log.Fatal("no models in schema")
		}
		if success {
			ok++
			if *verbose {
				fmt.Printf("[%d] %s ok\n", i+1, label)
			}
		} else {
			fail++
			if *verbose {
				fmt.Printf("[%d] %s fail: %s\n", i+1, label, detail)
			}
		}
	}
	fmt.Printf("steps=%d ok=%d fail=%d skip=%d pooled_ids=%d\n", *n, ok, fail, skip, r.Pool.TotalIDs())
}
