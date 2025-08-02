package googlesearch

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/corpix/uarand"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type SearchResult struct {
	URL         string
	Title       string
	Description string
}

func (sr SearchResult) String() string {
	return fmt.Sprintf("SearchResult(url=%s, title=%s, description=%s)", sr.URL, sr.Title, sr.Description)
}


func GetCustomUserAgent() string {
	lynx := fmt.Sprintf("Lynx/%d.%d.%d", 
		rand.Intn(2)+2,
		rand.Intn(2)+8,
		rand.Intn(3))
	
	libwww := fmt.Sprintf("libwww-FM/%d.%d", 
		rand.Intn(2)+2,
		rand.Intn(3)+13)
	
	sslmm := fmt.Sprintf("SSL-MM/%d.%d", 
		rand.Intn(2)+1,
		rand.Intn(3)+3)
	
	openssl := fmt.Sprintf("OpenSSL/%d.%d.%d", 
		rand.Intn(3)+1,
		rand.Intn(5),
		rand.Intn(10))
	
	return fmt.Sprintf("%s %s %s %s", lynx, libwww, sslmm, openssl)
}

func getRandomUserAgent() string {
	return uarand.GetRandom()
}

func sendRequest(term string, results int, lang string, start int, client *http.Client, region, safe string, sslVerify bool) (*http.Response, error) {
	baseURL := "https://www.google.com/search"
	req, err := http.NewRequest("GET", baseURL, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("q", term)
	q.Add("num", fmt.Sprintf("%d", results+2))
	q.Add("hl", lang)
	q.Add("start", fmt.Sprintf("%d", start))
	q.Add("safe", safe)
	if region != "" {
		q.Add("gl", region)
	}
	req.URL.RawQuery = q.Encode()

	req.Header.Set("User-Agent", getRandomUserAgent())
	req.Header.Set("Accept", "*/*")

	req.AddCookie(&http.Cookie{Name: "CONSENT", Value: "PENDING+987"})
	req.AddCookie(&http.Cookie{Name: "SOCS", Value: "CAESHAgBEhIaAB"})

	return client.Do(req)
}

func Search(
	term string,
	numResults int,
	lang string,
	proxy string,
	advanced bool,
	sleepInterval int,
	timeout int,
	safe string,
	sslVerify bool,
	region string,
	startNum int,
	unique bool,
) ([]interface{}, error) {
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, err
		}
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}

	if !sslVerify {
		client.Transport = &http.Transport{
			Proxy:           client.Transport.(*http.Transport).Proxy,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	if safe == "" {
		safe = "active"
	}

	start := startNum
	fetchedResults := 0
	fetchedLinks := make(map[string]bool)
	var results []interface{}

	for fetchedResults < numResults {
		resp, err := sendRequest(term, numResults, lang, start, client, region, safe, sslVerify)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("google: received non-200 status code: %d", resp.StatusCode)
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			return nil, err
		}

		newResults := 0
		doc.Find("div.ezO2md").Each(func(i int, s *goquery.Selection) {
			if fetchedResults >= numResults {
				return
			}

			linkTag := s.Find("a[href]").First()
			href, exists := linkTag.Attr("href")
			if !exists {
				return
			}

			if !strings.HasPrefix(href, "/url?q=") {
				return
			}
			link := strings.TrimPrefix(href, "/url?q=")
			if idx := strings.Index(link, "&"); idx != -1 {
				link = link[:idx]
			}
			decodedLink, err := url.QueryUnescape(link)
			if err != nil || decodedLink == "" {
				return
			}

			if unique && fetchedLinks[decodedLink] {
				return
			}
			fetchedLinks[decodedLink] = true

			title := linkTag.Find("span.CVA68e").First().Text()
			description := s.Find("span.FrIlee").First().Text()

			if advanced {
				results = append(results, SearchResult{
					URL:         decodedLink,
					Title:       title,
					Description: description,
				})
			} else {
				results = append(results, decodedLink)
			}

			fetchedResults++
			newResults++
		})

		if newResults == 0 {
			break
		}

		start += 10
		if sleepInterval > 0 {
			time.Sleep(time.Duration(sleepInterval) * time.Second)
		}
	}

	return results, nil
}
