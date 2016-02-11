package main

import "github.com/petejkim/rise-server/server"

func main() {
	r := server.New()
	r.Run(":3000")
}
