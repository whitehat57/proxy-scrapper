package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	maxConcurrentChecks = 200
	checkTimeout        = 10 * time.Second
	targetCheckURL      = "http://example.com"
	goodProxiesFile     = "good_proxies.txt"
)

// Sumber proxy gratis
var proxySources = []string{
	"https://free-proxy-list.net/",
	"https://www.sslproxies.org/",
	"https://www.us-proxy.org/",
	"https://www.proxyscan.io/",
}

func main() {
	log.Println("Memulai proses scraping proxy...")
	allProxies := scrapeAllProxies()
	log.Printf("Berhasil mengumpulkan %d proxy\n", len(allProxies))

	log.Println("Memulai pengecekan proxy...")
	goodProxies := checkProxiesConcurrently(allProxies)

	log.Printf("\nSelesai! Proxy baik: %d, Proxy mati: %d\n", len(goodProxies), len(allProxies)-len(goodProxies))
	saveProxiesToFile(goodProxies, goodProxiesFile)
	log.Printf("Proxy yang baik disimpan di %s\n", goodProxiesFile)
}

func scrapeAllProxies() []string {
	var allProxies []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, source := range proxySources {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			proxies, err := scrapeProxies(s)
			if err != nil {
				log.Printf("Gagal scrape %s: %v", s, err)
				return
			}
			
			mu.Lock()
			allProxies = append(allProxies, proxies...)
			mu.Unlock()
			log.Printf("Scrape %s: %d proxy", s, len(proxies))
		}(source)
	}
	wg.Wait()
	return deduplicateProxies(allProxies)
}

func scrapeProxies(url string) ([]string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code %d", resp.StatusCode)
	}

	return extractProxies(resp.Body)
}

func extractProxies(body io.Reader) ([]string, error) {
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, err
	}

	var proxies []string
	ipPortRegex := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}:\d{1,5}\b`)

	doc.Find("td").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if ipPortRegex.MatchString(text) {
			proxies = append(proxies, text)
		}
	})

	return proxies, nil
}

func deduplicateProxies(proxies []string) []string {
	unique := make(map[string]bool)
	var result []string
	for _, proxy := range proxies {
		if parts := strings.Split(proxy, ":"); len(parts) != 2 {
			continue
		}
		if _, exists := unique[proxy]; !exists {
			unique[proxy] = true
			result = append(result, proxy)
		}
	}
	return result
}

func checkProxiesConcurrently(proxies []string) []string {
	sem := make(chan struct{}, maxConcurrentChecks)
	results := make(chan string)
	var wg sync.WaitGroup
	var goodProxies []string

	// Hasil collector
	go func() {
		for proxy := range results {
			goodProxies = append(goodProxies, proxy)
		}
	}()

	// Worker
	for _, proxy := range proxies {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			sem <- struct{}{}
			if alive := checkProxy(p); alive {
				results <- p
				fmt.Printf("[✓] %s\n", p)
			} else {
				fmt.Printf("[✗] %s\n", p)
			}
			<-sem
		}(proxy)
	}

	wg.Wait()
	close(results)
	return goodProxies
}

func checkProxy(proxy string) bool {
	proxyURL, err := url.Parse("http://" + proxy)
	if err != nil {
		return false
	}

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   checkTimeout,
	}

	// Double check dengan 2 request berbeda
	check1 := checkWithClient(client, "http://example.com")
	check2 := checkWithClient(client, "http://httpbin.org/ip")
	
	return check1 && check2
}

func checkWithClient(client *http.Client, url string) bool {
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func saveProxiesToFile(proxies []string, filename string) {
	content := strings.Join(proxies, "\n")
	err := os.WriteFile(filename, []byte(content), 0644)
	if err != nil {
		log.Fatalf("Gagal menyimpan file: %v", err)
	}
}
