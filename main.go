package main

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/valyala/fasthttp"
)

type ErrorResponse struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`
}

type cachedData struct {
	Value     string
	ExpiresAt time.Time
}

type HTTPError struct {
	Code int
	Body string
}

var (
	cache = struct {
		sync.RWMutex
		data          map[string]cachedData
		cacheLifetime time.Duration
	}{data: make(map[string]cachedData), cacheLifetime: time.Minute}

	json        = jsoniter.ConfigCompatibleWithStandardLibrary
	serverIndex int
)

func main() {
	server := &fasthttp.Server{
		Handler:        handleRequests,
		ReadBufferSize: 8192,
	}

	fmt.Println("Server listening on :9001...")
	if err := server.ListenAndServe(":9001"); err != nil {
		fmt.Printf("Error: %s\n", err)
	}
}

func handleRequests(ctx *fasthttp.RequestCtx) {
	ctx.Response.Header.Set("Access-Control-Allow-Origin", "*")
	ctx.Response.Header.Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept")

	urlQueryParam := strings.ReplaceAll(string(ctx.QueryArgs().QueryString()), "url=", "")
	urlQueryParam, _ = url.QueryUnescape(urlQueryParam)
	decodedURL, err := url.QueryUnescape(urlQueryParam)

	if err != nil || decodedURL == "" {
		sendJSONErrorResponse(ctx, "Invalid or missing URL parameter", fasthttp.StatusBadRequest)
		return
	}

	servers, err := readServerAddresses("servers.txt")
	if err != nil {
		sendJSONErrorResponse(ctx, err.Error(), fasthttp.StatusInternalServerError)
		return
	}

	var finalResponse string
	var lastError error

	if time.Now().After(time.Now().Add(-3 * time.Minute)) {
		serverIndex = 0
	}

	for i := serverIndex; i < len(servers); i++ {
		encodedURL := url.QueryEscape(decodedURL)
		fmt.Printf("Request %d: %s%s\n", i+1, servers[i], fmt.Sprintf("/?url=%s", encodedURL))
		finalResponse, err = makeRequest(servers[i], fmt.Sprintf("/?url=%s", encodedURL))

		if err == nil {
			serverIndex = (i + 1) % len(servers)
			break
		}

		lastError = err
		if strings.Contains(err.Error(), "Ratelimit") || strings.Contains(err.Error(), "CAPTCHA") {
			continue
		}

		statusCode, body := parseHTTPError(lastError)
		sendJSONErrorResponse(ctx, body, statusCode)
		return
	}

	if lastError != nil {
		statusCode, body := parseHTTPError(lastError)
		sendJSONErrorResponse(ctx, body, statusCode)
		return
	}

	ctx.Write([]byte(finalResponse))
}

func sendJSONErrorResponse(ctx *fasthttp.RequestCtx, message string, statusCode int) {
	ctx.Response.Header.Set("Content-Type", "application/json")
	ctx.Response.SetStatusCode(statusCode)
	errorResponse := ErrorResponse{Message: message, Code: statusCode}
	jsonResponse, err := json.Marshal(errorResponse)
	if err != nil {
		ctx.Error("Internal Server Error", fasthttp.StatusInternalServerError)
		return
	}
	ctx.Write(jsonResponse)
}

func makeRequest(serverURL string, endpoint string) (string, error) {
	cacheKey := fmt.Sprintf("%s%s", serverURL, endpoint)

	if cachedData, ok := cacheGet(cacheKey); ok {
		return cachedData, nil
	}

	requestURL := fmt.Sprintf("%s%s", serverURL, endpoint)
	statusCode, body, err := fasthttp.Get(nil, requestURL)

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			fmt.Printf("DNS resolution timeout, moving to the next server.\n")
			return "", fmt.Errorf("DNS resolution timeout: %v", err)
		}

		if statusCode == fasthttp.StatusTooManyRequests || statusCode == 429 || statusCode == 420 || strings.Contains(err.Error(), "CAPTCHA") {
			fmt.Printf("Ratelimit or CAPTCHA error: %v\n", err)
			return "", fmt.Errorf("Ratelimit or CAPTCHA error: %v", err)
		}
		fmt.Printf("Unexpected error: %v\n", err)
		return "", fmt.Errorf("Unexpected error: %v", err)
	}

	if statusCode != fasthttp.StatusOK {
		fmt.Printf("Unexpected status code: %d\n", statusCode)
		if statusCode == fasthttp.StatusTooManyRequests || statusCode == 429 || statusCode == 420 || strings.Contains(string(body), "CAPTCHA") {
			fmt.Println("Ratelimit or CAPTCHA error, moving to the next server.")
			return "", fmt.Errorf("Ratelimit or CAPTCHA error: Unexpected status code: %d", statusCode)
		}
		return "", &HTTPError{Code: statusCode, Body: string(body)}
	}

	cacheSet(cacheKey, string(body))

	return string(body), nil
}

func (e *HTTPError) Error() string {
	return e.Body
}

func parseHTTPError(err error) (int, string) {
	if httpErr, ok := err.(*HTTPError); ok {
		return httpErr.Code, httpErr.Body
	}
	return fasthttp.StatusInternalServerError, err.Error()
}

func cacheGet(key string) (string, bool) {
	cache.RLock()
	defer cache.RUnlock()

	data, ok := cache.data[key]
	if !ok || time.Now().After(data.ExpiresAt) {
		return "", false
	}

	return data.Value, true
}

func cacheSet(key string, value string) {
	cache.Lock()
	defer cache.Unlock()

	expiresAt := time.Now().Add(cache.cacheLifetime)

	cache.data[key] = cachedData{
		Value:     value,
		ExpiresAt: expiresAt,
	}
}

func readServerAddresses(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var servers []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		servers = append(servers, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return servers, nil
}
