package main

import (
	"fmt"
	"time"

	"github.com/mr-karan/balance"
	"github.com/spf13/pflag"
	"github.com/valyala/fasthttp"
)

const (
	Version                  = "1.0"
	BadFailedRate            = 0.5
	BadAverageResponseTimeMs = 1900.0
	TestRounds               = 3
	TestRequestsPerRound     = 5
)

var upstreamSender = fasthttp.Client{
	MaxConnWaitTimeout: 5 * time.Second,
	ReadTimeout:        5 * time.Second,
	WriteTimeout:       5 * time.Second,
}

func getBalancerWithValidUpstreams(upstreams []Upstream) *balance.Balance {
	balancer := balance.NewBalance()

	for _, upstream := range upstreams {
		fmt.Printf("Testing %s:%d...", upstream.Host, upstream.Port)
		quality := upstream.Test(TestRounds, TestRequestsPerRound)
		fmt.Printf(" quality=(fails=%f, avg=%f)", quality.FailedRate, quality.AverageResponseTimeMs)
		if quality.FailedRate > BadFailedRate {
			fmt.Printf(" => bad upstream (fails > %v)\n", BadFailedRate)
		} else if quality.AverageResponseTimeMs > BadAverageResponseTimeMs {
			fmt.Printf(" => bad upstream (avg > %v)\n", BadAverageResponseTimeMs)
		} else {
			fmt.Printf(" => OK")
			err := balancer.Add(upstream.Base(), upstream.Weight)
			if err != nil {
				fmt.Printf(", but cannot add to balancer: %v\n", err)
			} else {
				fmt.Printf(", added to balancer\n")
			}
		}
	}

	return balancer
}

func handle(upstream string, ctx *fasthttp.RequestCtx) {
	method := string(ctx.Method())
	path := string(ctx.Path())

	bodyReader := ctx.Request.BodyStream()

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.Header.SetMethod(method)
	req.SetRequestURI(upstream + path)
	for k, v := range ctx.Request.Header.All() {
		req.Header.SetBytesKV(k, v)
	}
	req.SetBodyStream(bodyReader, ctx.Request.Header.ContentLength())

	if err := upstreamSender.Do(req, resp); err != nil {
		fmt.Printf("%s %s /-->/ %s (%v)\n", method, path, upstream, err)

		ctx.Error(fmt.Sprintf("LiveProxy error: %v", err), fasthttp.StatusBadGateway)
		return
	}

	ctx.Response.SetStatusCode(resp.StatusCode())
	for k, v := range resp.Header.All() {
		ctx.Response.Header.SetBytesKV(k, v)
	}

	fmt.Printf("%s %s --> %s\n", method, path, upstream)
}

func main() {
	fmt.Printf("--- :LiveProxy %s: ---\n\n", Version)
	fmt.Printf("Parsing CLI arguments --host, --port, --upstream {weight}:{host}:{port}...\n")

	host := pflag.String("host", "localhost", "listen host")
	port := pflag.Uint16("port", 8080, "listen port")
	upstreamEntries := pflag.StringArray("upstream", make([]string, 0), "{weight}:{host}:{port}")
	pflag.Parse()

	fmt.Printf("%d upstreams found, checking...\n", len(*upstreamEntries))

	upstreams := ParseManyFromString(*upstreamEntries, func(mangled string, err error) {
		fmt.Printf("%s ignored (parsing error): %s\n", mangled, err)
	})
	balancer := getBalancerWithValidUpstreams(upstreams)
	listen := fmt.Sprintf("%s:%d", *host, *port)

	fmt.Printf("Tests completed. Valid upstreams count: %d\n", len(balancer.ItemIDs()))
	fmt.Printf("Starting FastHTTP server at %s\n", listen)

	if err := fasthttp.ListenAndServe(
		listen,
		func(ctx *fasthttp.RequestCtx) {
			handle(balancer.Get(), ctx)
		},
	); err != nil {
		fmt.Printf("Error in ListenAndServe: %v\n", err)
	}
}
