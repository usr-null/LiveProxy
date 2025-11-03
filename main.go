package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mr-karan/balance"
	"github.com/spf13/pflag"
)

const VERSION = "1.0"

func main() {
	fmt.Printf("--- :LiveProxy %s: ---\n", VERSION)
	fmt.Println("--- Parsing CLI arguments --backend<weight> ---")

	balancer := balance.NewBalance()

	i := 0
	for {
		i++

		full := fmt.Sprintf("backend%d", i)
		usage := fmt.Sprintf("--backend<id> <weight:host:port>")

		arg := pflag.StringP(full, "", "", usage)

		if arg == nil || *arg == "" {
			break
		}

		segments := strings.Split(*arg, ":")
		if len(segments) != 3 {
			fmt.Printf("%s: invalid backend, format: weight:host:port\n", *arg)
			continue
		}

		weight, err := strconv.Atoi(segments[0])
		if err != nil {
			fmt.Printf("%s: invalid weight: %s\n", *arg, err)
			continue
		}
		host := segments[1]
		port, err := strconv.Atoi(segments[1])
		if err != nil {
			fmt.Printf("%s: invalid port: %s\n", *arg, err)
			continue
		}

		target := fmt.Sprintf("%s:%d/", host, port)

		fmt.Printf("Checking with GET %s... ", target)

		_, err = http.Get(target)
		if err != nil {
			fmt.Println("error")
			continue
		}
		fmt.Println("OK")

		err = balancer.Add(target, weight)
		if err != nil {
			fmt.Printf("%s: cannot add to load balancer %v\n", *arg, err)
		}
	}

	r := gin.Default()
	_ = r.SetTrustedProxies(nil)

	// Define a simple GET endpoint
	r.GET("/*path", func(c *gin.Context) {

		c.JSON(http.StatusOK, gin.H{
			"path": c.Param("path"),
		})
	})

	r.Run()
}
