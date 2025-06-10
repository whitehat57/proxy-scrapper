package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

type ProxySource struct {
	Name string
	URL  string
}

type Proxy struct {
	IP   string
	Port string
	Full string
}

type ProxyChecker struct {
	timeout       time.Duration
	maxWorkers    int
	validProxies  []Proxy
	invalidProxies []Proxy
	mu            sync.RWMutex
	wg            sync.WaitGroup
}

// Daftar sumber proxy gratis
var proxySources = []ProxySource{
	{"ProxyList-1", "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt"},
	{"ProxyList-2", "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt"},
	{"ProxyList-3", "https://raw.githubusercontent.com/clarketm/proxy-list/master/proxy-list-raw.txt"},
	{"ProxyList-4", "https://raw.githubusercontent.com/sunny9577/proxy-scraper/master/proxies.txt"},
	{"ProxyList-5", "https://raw.githubusercontent.com/ShiftyTR/Proxy-List/master/http.txt"},
	{"ProxyList-6", "https://raw.githubusercontent.com/roosterkid/openproxylist/main/HTTPS_RAW.txt"},
	{"ProxyList-7", "https://raw.githubusercontent.com/mmpx12/proxy-list/master/http.txt"},
	{"ProxyList-8", "https://raw.githubusercontent.com/proxy4parsing/proxy-list/main/http.txt"},
}

func NewProxyChecker(timeout time.Duration, maxWorkers int) *ProxyChecker {
	return &ProxyChecker{
		timeout:    timeout,
		maxWorkers: maxWorkers,
	}
}

func main() {
	fmt.Println("🚀 Memulai Proxy Scraper dan Validator")
	fmt.Println("=====================================")

	checker := NewProxyChecker(10*time.Second, 100) // 10 detik timeout, 100 goroutines
	
	// Scrape proxies dari semua sumber
	allProxies := scrapeAllProxies()
	if len(allProxies) == 0 {
		log.Fatal("❌ Tidak ada proxy yang berhasil di-scrape")
	}

	fmt.Printf("📊 Total proxy yang ditemukan: %d\n", len(allProxies))
	fmt.Println("🔍 Memulai pengecekan proxy...")
	fmt.Println("=====================================")

	// Validasi semua proxy
	checker.validateProxies(allProxies)

	// Simpan hasil ke file
	err := saveProxiesToFile(checker.validProxies, "claude_valid_proxies.txt")
	if err != nil {
		log.Printf("❌ Error menyimpan proxy valid: %v", err)
	}

	err = saveProxiesToFile(checker.invalidProxies, "claude_invalid_proxies.txt")
	if err != nil {
		log.Printf("❌ Error menyimpan proxy invalid: %v", err)
	}

	// Tampilkan ringkasan
	fmt.Println("\n=====================================")
	fmt.Println("📊 RINGKASAN HASIL")
	fmt.Println("=====================================")
	fmt.Printf("✅ Proxy Valid: %d\n", len(checker.validProxies))
	fmt.Printf("❌ Proxy Invalid: %d\n", len(checker.invalidProxies))
	fmt.Printf("📁 Proxy valid disimpan di: claude_valid_proxies.txt\n")
	fmt.Printf("📁 Proxy invalid disimpan di: claude_invalid_proxies.txt\n")
}

func scrapeAllProxies() []Proxy {
	var allProxies []Proxy
	var mu sync.Mutex
	var wg sync.WaitGroup

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	for _, source := range proxySources {
		wg.Add(1)
		go func(src ProxySource) {
			defer wg.Done()
			
			fmt.Printf("🌐 Scraping dari %s...\n", src.Name)
			proxies, err := scrapeProxiesFromURL(client, src.URL)
			if err != nil {
				log.Printf("❌ Error scraping dari %s: %v", src.Name, err)
				return
			}

			mu.Lock()
			allProxies = append(allProxies, proxies...)
			mu.Unlock()
			
			fmt.Printf("✅ Berhasil scrape %d proxy dari %s\n", len(proxies), src.Name)
		}(source)
	}

	wg.Wait()
	
	// Hapus duplikat
	uniqueProxies := removeDuplicateProxies(allProxies)
	fmt.Printf("🧹 Setelah menghapus duplikat: %d proxy\n", len(uniqueProxies))
	
	return uniqueProxies
}

func scrapeProxiesFromURL(client *http.Client, url string) ([]Proxy, error) {
	// Retry mechanism dengan double check
	var resp *http.Response
	var err error
	
	for i := 0; i < 3; i++ { // 3 kali percobaan
		resp, err = client.Get(url)
		if err == nil && resp.StatusCode == 200 {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(time.Duration(i+1) * time.Second) // Backoff
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to fetch after 3 attempts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return parseProxies(string(body)), nil
}

func parseProxies(content string) []Proxy {
	var proxies []Proxy
	
	// Regex untuk menangkap format IP:Port
	re := regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):(\d{1,5})`)
	matches := re.FindAllStringSubmatch(content, -1)
	
	for _, match := range matches {
		if len(match) >= 3 {
			ip := match[1]
			port := match[2]
			
			// Validasi IP dan Port
			if isValidIP(ip) && isValidPort(port) {
				proxy := Proxy{
					IP:   ip,
					Port: port,
					Full: fmt.Sprintf("%s:%s", ip, port),
				}
				proxies = append(proxies, proxy)
			}
		}
	}
	
	return proxies
}

func isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

func isValidPort(port string) bool {
	// Port harus antara 1-65535
	if len(port) == 0 || len(port) > 5 {
		return false
	}
	for _, char := range port {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func removeDuplicateProxies(proxies []Proxy) []Proxy {
	seen := make(map[string]bool)
	var unique []Proxy
	
	for _, proxy := range proxies {
		if !seen[proxy.Full] {
			seen[proxy.Full] = true
			unique = append(unique, proxy)
		}
	}
	
	return unique
}

func (pc *ProxyChecker) validateProxies(proxies []Proxy) {
	jobs := make(chan Proxy, len(proxies))
	
	// Start workers
	for i := 0; i < pc.maxWorkers; i++ {
		pc.wg.Add(1)
		go pc.worker(jobs)
	}
	
	// Send jobs
	for _, proxy := range proxies {
		jobs <- proxy
	}
	close(jobs)
	
	// Wait for all workers to finish
	pc.wg.Wait()
}

func (pc *ProxyChecker) worker(jobs <-chan Proxy) {
	defer pc.wg.Done()
	
	for proxy := range jobs {
		if pc.checkProxy(proxy) {
			pc.mu.Lock()
			pc.validProxies = append(pc.validProxies, proxy)
			pc.mu.Unlock()
			fmt.Printf("✅ VALID: %s\n", proxy.Full)
		} else {
			pc.mu.Lock()
			pc.invalidProxies = append(pc.invalidProxies, proxy)
			pc.mu.Unlock()
			fmt.Printf("❌ INVALID: %s\n", proxy.Full)
		}
	}
}

func (pc *ProxyChecker) checkProxy(proxy Proxy) bool {
	// Double check dengan 2 test URL berbeda
	testURLs := []string{
		"http://httpbin.org/ip",
		"http://icanhazip.com",
	}
	
	successCount := 0
	
	for _, testURL := range testURLs {
		if pc.testProxyConnection(proxy, testURL) {
			successCount++
		}
	}
	
	// Proxy dianggap valid jika minimal 1 dari 2 test berhasil
	return successCount >= 1
}

func (pc *ProxyChecker) testProxyConnection(proxy Proxy, testURL string) bool {
	// Context dengan timeout
	ctx, cancel := context.WithTimeout(context.Background(), pc.timeout)
	defer cancel()
	
	// Setup proxy client
	proxyURL := fmt.Sprintf("http://%s", proxy.Full)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*http.URL, error) {
				return http.ParseURL(proxyURL)
			},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, network, addr)
			},
		},
		Timeout: pc.timeout,
	}
	
	// Buat request dengan context
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return false
	}
	
	// Set header untuk menghindari deteksi bot
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	
	// Kirim request
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	// Check status code
	if resp.StatusCode != 200 {
		return false
	}
	
	// Baca response untuk memastikan proxy bekerja
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	
	// Response harus mengandung IP address
	return len(strings.TrimSpace(string(body))) > 0
}

func saveProxiesToFile(proxies []Proxy, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filename, err)
	}
	defer file.Close()
	
	writer := bufio.NewWriter(file)
	defer writer.Flush()
	
	for _, proxy := range proxies {
		_, err := writer.WriteString(proxy.Full + "\n")
		if err != nil {
			return fmt.Errorf("failed to write to file: %w", err)
		}
	}
	
	return nil
}
