package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	maxConcurrentChecks = 200
	checkTimeout        = 10 * time.Second
	scrapeTimeout       = 30 * time.Second
	goodProxiesFile     = "good_proxies.txt"
)

// Sumber proxy gratis dengan parser khusus
var proxySources = []struct {
	URL    string
	Parser func(*goquery.Document) []string
}{
	{
		URL: "https://free-proxy-list.net/",
		Parser: func(doc *goquery.Document) []string {
			var proxies []string
			doc.Find("table#proxylisttable tbody tr").Each(func(i int, s *goquery.Selection) {
				ip := s.Find("td:nth-child(1)").Text()
				port := s.Find("td:nth-child(2)").Text()
				if ip != "" && port != "" {
					proxies = append(proxies, ip+":"+port)
				}
			})
			return proxies
		},
	},
	{
		URL: "https://www.sslproxies.org/",
		Parser: func(doc *goquery.Document) []string {
			var proxies []string
			doc.Find("table#proxylisttable tbody tr").Each(func(i int, s *goquery.Selection) {
				ip := s.Find("td:nth-child(1)").Text()
				port := s.Find("td:nth-child(2)").Text()
				if ip != "" && port != "" {
					proxies = append(proxies, ip+":"+port)
				}
			})
			return proxies
		},
	},
	{
		URL: "https://www.us-proxy.org/",
		Parser: func(doc *goquery.Document) []string {
			var proxies []string
			doc.Find("table#proxylisttable tbody tr").Each(func(i int, s *goquery.Selection) {
				ip := s.Find("td:nth-child(1)").Text()
				port := s.Find("td:nth-child(2)").Text()
				if ip != "" && port != "" {
					proxies = append(proxies, ip+":"+port)
				}
			})
			return proxies
		},
	},
	{
		URL: "https://proxyscrape.com/free-proxy-list",
		Parser: func(doc *goquery.Document) []string {
			var proxies []string
			doc.Find("table.table tbody tr").Each(func(i int, s *goquery.Selection) {
				ip := s.Find("td:nth-child(1)").Text()
				port := s.Find("td:nth-child(2)").Text()
				if ip != "" && port != "" {
					proxies = append(proxies, ip+":"+port)
				}
			})
			return proxies
		},
	},
	{
		URL: "https://proxylist.geonode.com/api/proxy-list?limit=500&page=1&sort_by=lastChecked&sort_type=desc",
		Parser: func(doc *goquery.Document) []string {
			// Situs ini mengembalikan JSON, kita akan tangani secara khusus
			return []string{}
		},
	},
}

func main() {
	log.Println("Memulai proses scraping proxy...")
	allProxies := scrapeAllProxies()
	log.Printf("Berhasil mengumpulkan %d proxy\n", len(allProxies))

	if len(allProxies) == 0 {
		log.Println("Tidak ada proxy yang ditemukan, keluar...")
		return
	}

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
		go func(s struct {
			URL    string
			Parser func(*goquery.Document) []string
		}) {
			defer wg.Done()
			proxies, err := scrapeProxies(s.URL, s.Parser)
			if err != nil {
				log.Printf("Gagal scrape %s: %v", s.URL, err)
				return
			}
			
			mu.Lock()
			allProxies = append(allProxies, proxies...)
			mu.Unlock()
			log.Printf("Scrape %s: %d proxy", s.URL, len(proxies))
		}(source)
	}
	wg.Wait()
	return deduplicateProxies(allProxies)
}

func scrapeProxies(url string, parser func(*goquery.Document) []string) ([]string, error) {
	client := &http.Client{Timeout: scrapeTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	// Set header untuk menyerupai browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/101.0.4951.64 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code %d", resp.StatusCode)
	}

	// Handle khusus untuk API JSON (GeoNode)
	if strings.Contains(url, "geonode.com") {
		return scrapeGeoNodeProxies(resp)
	}

	// Parsing HTML untuk situs lainnya
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	return parser(doc), nil
}

func scrapeGeoNodeProxies(resp *http.Response) ([]string, error) {
	type geoNodeProxy struct {
		IP   string `json:"ip"`
		Port string `json:"port"`
	}

	type geoNodeResponse struct {
		Data []geoNodeProxy `json:"data"`
	}

	var result geoNodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var proxies []string
	for _, p := range result.Data {
		if p.IP != "" && p.Port != "" {
			proxies = append(proxies, p.IP+":"+p.Port)
		}
	}
	return proxies, nil
}

func deduplicateProxies(proxies []string) []string {
	unique := make(map[string]bool)
	var result []string
	for _, proxy := range proxies {
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
	var mu sync.Mutex

	// Worker
	for _, proxy := range proxies {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			sem <- struct{}{}
			alive := checkProxy(p)
			<-sem

			if alive {
				mu.Lock()
				goodProxies = append(goodProxies, p)
				mu.Unlock()
				fmt.Printf("[✓] %s\n", p)
			} else {
				fmt.Printf("[✗] %s\n", p)
			}
		}(proxy)
	}

	wg.Wait()
	return goodProxies
}

func checkProxy(proxy string) bool {
	proxyURL, err := url.Parse("http://" + proxy)
	if err != nil {
		return false
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			// Disable keep-alive untuk koneksi lebih cepat
			DisableKeepAlives: true,
		},
		Timeout: checkTimeout,
	}

	// Coba dengan beberapa target berbeda
	targets := []string{
		"http://example.com",
		"http://httpbin.org/ip",
		"http://google.com",
	}

	successCount := 0
	for _, target := range targets {
		if checkWithClient(client, target) {
			successCount++
		}
		// Beri jeda singkat antara pengecekan
		time.Sleep(100 * time.Millisecond)
	}

	// Dianggap valid jika berhasil di 2 dari 3 target
	return successCount >= 2
}

func checkWithClient(client *http.Client, url string) bool {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}

	// Set timeout khusus untuk request
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func saveProxiesToFile(proxies []string, filename string) {
	if len(proxies) == 0 {
		log.Println("Tidak ada proxy yang valid untuk disimpan")
		return
	}

	content := strings.Join(proxies, "\n")
	err := os.WriteFile(filename, []byte(content), 0644)
	if err != nil {
		log.Printf("Gagal menyimpan file: %v", err)
	}
}