package main

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

var testsClient = fasthttp.Client{
	MaxConnWaitTimeout: 3 * time.Second,
	ReadTimeout:        3 * time.Second,
	WriteTimeout:       3 * time.Second,
}

type Upstream struct {
	Secure bool
	Host   string
	Port   uint16
	Weight int
}

type UpstreamQuality struct {
	FailedRate            float64
	AverageResponseTimeMs float64
}

var BlackholeUpstream = Upstream{
	Host:   "",
	Port:   0,
	Weight: math.MaxInt,
}

var protocolRegexp = regexp.MustCompile("(.+?)://")

func (upstream *Upstream) IsBlackhole() bool {
	return upstream.Weight == math.MaxInt && upstream.Host == "" && upstream.Port == 0
}

func (upstream *Upstream) Base() string {
	if upstream.IsBlackhole() {
		return "http://0"
	}

	protocol := "http"
	if upstream.Secure {
		protocol = "https"
	}

	return fmt.Sprintf("%s://%s:%d", protocol, upstream.Host, upstream.Port)
}

func (upstream *Upstream) Test(rounds int, requestsPerRound int) UpstreamQuality {
	if upstream.IsBlackhole() {
		return UpstreamQuality{
			FailedRate:            math.Inf(1),
			AverageResponseTimeMs: math.Inf(1),
		}
	}

	reqSends := 0
	reqFails := 0
	reqTimesSum := 0

	for i := 0; i < rounds; i++ {
		var wg sync.WaitGroup

		for j := 0; j < requestsPerRound; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				start := time.Now().UnixNano()

				reqSends++
				_, _, err := testsClient.Get(nil, upstream.Base())
				if err != nil {
					reqFails++
					return
				}

				reqTimesSum += int(time.Now().UnixNano()-start) / 1_000_000
			}()
		}

		wg.Wait()
	}

	return UpstreamQuality{
		FailedRate:            float64(reqFails) / float64(reqSends),
		AverageResponseTimeMs: float64(reqTimesSum) / float64(reqSends),
	}
}

type ParseErrorCallback func(mangled string, err error)

func ParseOneFromString(mangled string) (Upstream, error) {
	fragments := strings.Split(mangled, ":")

	if len(fragments) < 3 {
		return BlackholeUpstream, errors.New("too few mangled fragments")
	}
	if len(fragments) > 4 {
		return BlackholeUpstream, errors.New("too many mangled fragments")
	}

	weightStr := fragments[0]

	protocol := fragments[1]
	if protocol != "http" && protocol != "https" {
		return BlackholeUpstream, errors.New("invalid protocol")
	}

	host := fragments[2]
	secure := protocol == "https"
	host = protocolRegexp.ReplaceAllString(host, "")

	var port uint64 = 80
	if secure {
		port = 443
	}

	weight, err := strconv.ParseInt(weightStr, 10, 32)
	if err != nil {
		return BlackholeUpstream, fmt.Errorf("weight %s is not a float", weightStr)
	}

	if len(fragments) == 4 {
		portStr := fragments[3]
		port, err = strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return BlackholeUpstream, fmt.Errorf("port %s is not an unsigned short int", portStr)
		}
	}
	return Upstream{secure, host, uint16(port), int(weight)}, nil
}

func ParseManyFromString(mangledEntries []string, onError ParseErrorCallback) []Upstream {
	result := make([]Upstream, 0)

	for _, mangled := range mangledEntries {
		upstream, err := ParseOneFromString(mangled)
		if err != nil {
			onError(mangled, err)
			continue
		}
		result = append(result, upstream)
	}

	return result
}
