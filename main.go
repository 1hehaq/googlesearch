package googlesearch

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type SearchResult struct {
	URL         string
	Title       string
	Description string
}

type SearchOptions struct {
	Language      string        // language code (default: "en")
	Region        string        // region code (default: "")
	SafeSearch    string        // safe search setting: "active", "off" (default: "active")
	Start         int           // starting result number (default: 0)
	SleepInterval time.Duration // delay between requests (default: 0)
	Timeout       time.Duration // request timeout (default: 10s)
	UserAgent     string        // custom user agent (default: random)
	Proxy         string        // proxy URL (default: "")
	Unique        bool          // filter duplicate URLs (default: false)
}

func DefaultOptions() *SearchOptions {
	return &SearchOptions{
		Language:      "en",
		Region:        "",
		SafeSearch:    "active",
		Start:         0,
		SleepInterval: 0,
		Timeout:       10 * time.Second,
		UserAgent:     getUserAgent(),
		Proxy:         "",
		Unique:        false,
	}
}

func Search(query string, numResults int, opts ...*SearchOptions) ([]string, error) {
	var results []string
	for result := range SearchChan(query, numResults, opts...) {
		if result.Error != nil {
			return results, result.Error
		}
		results = append(results, result.URL)
	}
	return results, nil
}

func SearchAdvanced(query string, numResults int, opts ...*SearchOptions) ([]SearchResult, error) {
	var results []SearchResult
	for result := range SearchAdvancedChan(query, numResults, opts...) {
		if result.Error != nil {
			return results, result.Error
		}
		results = append(results, SearchResult{
			URL:         result.URL,
			Title:       result.Title,
			Description: result.Description,
		})
	}
	return results, nil
}

type SearchResponse struct {
	URL         string
	Title       string
	Description string
	Error       error
}

func SearchChan(query string, numResults int, opts ...*SearchOptions) <-chan SearchResponse {
	ch := make(chan SearchResponse)
	go func() {
		defer close(ch)
		for result := range SearchAdvancedChan(query, numResults, opts...) {
			ch <- SearchResponse{
				URL:   result.URL,
				Error: result.Error,
			}
		}
	}()
	return ch
}

func SearchAdvancedChan(query string, numResults int, opts ...*SearchOptions) <-chan SearchResponse {
	ch := make(chan SearchResponse)
	
	go func() {
		defer close(ch)
		
		var options *SearchOptions
		if len(opts) > 0 && opts[0] != nil {
			options = opts[0]
		} else {
			options = DefaultOptions()
		}
		
		client := &http.Client{
			Timeout: options.Timeout,
		}
		
		if options.Proxy != "" {
			proxyURL, err := url.Parse(options.Proxy)
			if err == nil {
				client.Transport = &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
				}
			}
		}
		
		start := options.Start
		fetchedResults := 0
		fetchedLinks := make(map[string]bool)
		
		for fetchedResults < numResults {
			resp, err := makeRequest(client, query, numResults-start, options.Language, 
				start, options.SafeSearch, options.Region, options.UserAgent)
			if err != nil {
				ch <- SearchResponse{Error: err}
				return
			}
			
			results, err := parseResults(resp)
			if err != nil {
				ch <- SearchResponse{Error: err}
				return
			}
			
			newResults := 0
			for _, result := range results {
				if options.Unique && fetchedLinks[result.URL] {
					continue
				}
				
				fetchedLinks[result.URL] = true
				fetchedResults++
				newResults++
				
				ch <- SearchResponse{
					URL:         result.URL,
					Title:       result.Title,
					Description: result.Description,
				}
				
				if fetchedResults >= numResults {
					break
				}
			}
			
			if newResults == 0 {
				break
			}
			
			start += 10
			if options.SleepInterval > 0 {
				time.Sleep(options.SleepInterval)
			}
		}
	}()
	
	return ch
}

func makeRequest(client *http.Client, query string, numResults int, lang string, start int, safe string, region string, userAgent string) (string, error) {
	params := url.Values{
		"q":     {query},
		"num":   {strconv.Itoa(numResults + 2)},
		"hl":    {lang},
		"start": {strconv.Itoa(start)},
		"safe":  {safe},
	}
	
	if region != "" {
		params.Set("gl", region)
	}
	
	req, err := http.NewRequest("GET", "https://www.google.com/search?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	
	req.AddCookie(&http.Cookie{Name: "CONSENT", Value: "PENDING+987"})
	req.AddCookie(&http.Cookie{Name: "SOCS", Value: "CAESHAgBEhIaAB"})
	
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	
	doc, err := html.Parse(resp.Body)
	if err != nil {
		return "", err
	}
	
	return nodeToString(doc), nil
}

func parseResults(htmlContent string) ([]SearchResult, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}
	
	var results []SearchResult
	var traverse func(*html.Node)
	
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			if hasClass(n, "ezO2md") {
				result := extractResult(n)
				if result.URL != "" {
					results = append(results, result)
				}
			}
		}
		
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	
	traverse(doc)
	return results, nil
}

func extractResult(node *html.Node) SearchResult {
	var result SearchResult
	var traverse func(*html.Node)
	
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "a":
				if result.URL == "" {
					href := getAttr(n, "href")
					if href != "" && strings.HasPrefix(href, "/url?q=") {
						// Decode Google redirect URL
						parts := strings.Split(href, "&")
						if len(parts) > 0 {
							urlPart := strings.TrimPrefix(parts[0], "/url?q=")
							decoded, err := url.QueryUnescape(urlPart)
							if err == nil {
								result.URL = decoded
							}
						}
					}
				}
			case "span":
				if hasClass(n, "CVA68e") && result.Title == "" {
					result.Title = getTextContent(n)
				} else if hasClass(n, "FrIlee") && result.Description == "" {
					result.Description = getTextContent(n)
				}
			}
		}
		
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	
	traverse(node)
	return result
}

func hasClass(node *html.Node, className string) bool {
	for _, attr := range node.Attr {
		if attr.Key == "class" {
			classes := strings.Fields(attr.Val)
			for _, class := range classes {
				if class == className {
					return true
				}
			}
		}
	}
	return false
}

func getAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func getTextContent(node *html.Node) string {
	var text strings.Builder
	var traverse func(*html.Node)
	
	traverse = func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	
	traverse(node)
	return strings.TrimSpace(text.String())
}

func nodeToString(node *html.Node) string {
	var buf strings.Builder
	html.Render(&buf, node)
	return buf.String()
}

func getUserAgent() string {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:89.0) Gecko/20100101 Firefox/89.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:89.0) Gecko/20100101 Firefox/89.0",
	}
	return userAgents[time.Now().Unix()%int64(len(userAgents))]
}
