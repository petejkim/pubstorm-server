package main

import "github.com/nitrous-io/rise-server/apiserver/server"

func main() {
	r := server.New()
	r.Run(":3000")
}
