package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// --- KONFIGURASI ---

// workerCount adalah jumlah goroutine yang akan memeriksa proxy secara bersamaan.
// Nilai yang lebih tinggi akan mempercepat proses tetapi membutuhkan lebih banyak sumber daya.
// Sesuaikan dengan kemampuan jaringan dan CPU Anda.
const workerCount = 150

// checkTimeout adalah batas waktu untuk setiap permintaan pengecekan proxy.
// Jika proxy tidak merespons dalam waktu ini, ia dianggap mati.
const checkTimeout = 8 * time.Second

// outputFile adalah nama file untuk menyimpan daftar proxy yang aktif.
const outputFile = "gemini_live_proxies.txt"

// checkURL adalah URL target yang digunakan untuk memvalidasi proxy.
// Situs ini ideal karena ringan dan mengembalikan IP, cara yang bagus untuk
// memastikan proxy tidak hanya terhubung tetapi juga berfungsi dengan benar (tidak transparan).
const checkURL = "https://api.ipify.org"

// proxySources adalah daftar sumber proxy gratis.
// Sumber-sumber ini sering berubah atau tidak dapat diandalkan.
// Anda mungkin perlu mencari dan memperbarui daftar ini secara berkala untuk hasil terbaik.
var proxySources = []string{
	"https://api.proxyscrape.com/v2/?request=getproxies&protocol=http&timeout=10000&country=all&ssl=all&anonymity=all",
	"https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt",
	"https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt",
	"https://raw.githubusercontent.com/jetkai/proxy-list/main/online-proxies/txt/proxies-http.txt",
	"https://raw.githubusercontent.com/clarketm/proxy-list/master/proxy-list-raw.txt",
	"https://raw.githubusercontent.com/officialputuid/KangProxy/KangProxy/http/http.txt",
	"https://raw.githubusercontent.com/UptimerBot/proxy-list/main/proxies/http.txt",
}

// --- UTAMA ---

func main() {
	// Mengatur log untuk tampilan yang lebih bersih tanpa timestamp default.
	log.SetFlags(0)

	// Membersihkan file output lama jika ada.
	_ = os.Remove(outputFile)

	// Channel untuk menampung proxy yang di-scrape dari sumber.
	scrapedProxiesChan := make(chan string, workerCount*10)
	// Channel untuk menampung proxy yang lolos pengecekan.
	liveProxiesChan := make(chan string, workerCount)

	// WaitGroup untuk sinkronisasi goroutine.
	var wgScrapers sync.WaitGroup
	var wgCheckers sync.WaitGroup

	log.Println("🚀 Memulai proses scraping dan pengecekan proxy...")
	log.Printf("🔩 Konfigurasi: %d workers, %s timeout\n\n", workerCount, checkTimeout)

	// 1. Jalankan goroutine untuk mengumpulkan proxy yang aktif dan menyimpannya ke file.
	// Goroutine ini berjalan di latar belakang, menunggu proxy aktif masuk.
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go collectAndSaveLiveProxies(liveProxiesChan, &collectorWg)

	// 2. Jalankan goroutine pekerja (checker) sebanyak workerCount.
	// Mereka akan mengambil proxy dari scrapedProxiesChan dan memeriksanya.
	wgCheckers.Add(workerCount)
	for i := 1; i <= workerCount; i++ {
		go checkProxyWorker(i, scrapedProxiesChan, liveProxiesChan, &wgCheckers)
	}

	// 3. Scrape proxy dari semua sumber secara bersamaan.
	log.Printf("🔍 Scraping proxy dari %d sumber...\n", len(proxySources))
	for _, sourceURL := range proxySources {
		wgScrapers.Add(1)
		go scrapeProxies(sourceURL, scrapedProxiesChan, &wgScrapers)
	}

	// 4. Tunggu semua proses scraping selesai.
	wgScrapers.Wait()
	// Setelah scraping selesai, tutup scrapedProxiesChan.
	// Ini akan memberitahu para checker bahwa tidak ada lagi proxy yang akan datang.
	close(scrapedProxiesChan)
	log.Println("\n✅ Semua sumber telah selesai di-scrape.")

	// 5. Tunggu semua checker selesai bekerja.
	wgCheckers.Wait()
	// Setelah checker selesai, tutup liveProxiesChan.
	// Ini akan memberitahu kolektor untuk berhenti dan menyelesaikan penulisan file.
	close(liveProxiesChan)
	log.Println("✅ Semua proxy telah selesai dicek.")

	// 6. Tunggu goroutine kolektor selesai menulis file.
	collectorWg.Wait()

	log.Printf("\n🎉 Selesai! Proxy yang aktif disimpan di file: %s\n", outputFile)
}

// --- FUNGSI-FUNGSI ---

// scrapeProxies mengambil daftar proxy dari satu URL sumber dan mengirimkannya ke channel.
func scrapeProxies(sourceURL string, proxiesChan chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()

	resp, err := http.Get(sourceURL)
	if err != nil {
		log.Printf("   [SCRAPE GAGAL] %s: %v\n", sourceURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("   [SCRAPE GAGAL] %s: Status code %d\n", sourceURL, resp.StatusCode)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	count := 0
	for scanner.Scan() {
		proxy := strings.TrimSpace(scanner.Text())
		if proxy != "" {
			proxiesChan <- proxy
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("   [SCRAPE ERROR] %s: %v\n", sourceURL, err)
	} else {
		log.Printf("   [SCRAPE SUKSES] %d proxy dari %s\n", count, sourceURL)
	}
}

// checkProxyWorker adalah pekerja yang mengambil proxy dari channel, memeriksanya,
// dan mengirimkan yang aktif ke channel liveProxies.
func checkProxyWorker(id int, proxiesChan <-chan string, liveProxiesChan chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()

	// Terus bekerja selama channel 'proxiesChan' masih terbuka dan berisi data.
	for proxyAddr := range proxiesChan {
		// Log awal untuk menunjukkan proxy sedang diproses.
		// fmt.Printf("🤔 [Worker %d] Mengecek -> %s\n", id, proxyAddr)

		proxyURL, err := url.Parse("http://" + proxyAddr)
		if err != nil {
			// Jika format proxy tidak valid, lewati.
			continue
		}

		// Konfigurasi transport HTTP dengan proxy dan timeout
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}

		// Konfigurasi client HTTP
		client := &http.Client{
			Transport: transport,
			Timeout:   checkTimeout,
		}

		// Lakukan permintaan GET ke URL target untuk validasi.
		// Ini adalah "double check" kita: tidak hanya terhubung, tapi juga bisa mengambil konten.
		resp, err := client.Get(checkURL)
		if err != nil {
			// Jika ada error (timeout, koneksi ditolak, dll), proxy dianggap mati.
			// log.Printf("   ☠️ [MATI] %s: %v\n", proxyAddr, err)
			continue
		}
		resp.Body.Close() // Pastikan untuk selalu menutup body.

		// Pastikan status code adalah 200 OK.
		if resp.StatusCode == http.StatusOK {
			log.Printf("   ✔️ [AKTIF] %s\n", proxyAddr)
			liveProxiesChan <- proxyAddr
		}
	}
}

// collectAndSaveLiveProxies mengambil proxy aktif dari channel dan menyimpannya ke file.
func collectAndSaveLiveProxies(liveProxiesChan <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()

	file, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("❌ FATAL: Gagal membuat file output '%s': %v", outputFile, err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	uniqueProxies := make(map[string]bool)
	count := 0

	// Terus bekerja selama channel 'liveProxiesChan' masih terbuka.
	for proxy := range liveProxiesChan {
		// Pastikan tidak ada duplikat.
		if _, exists := uniqueProxies[proxy]; !exists {
			uniqueProxies[proxy] = true
			_, err := fmt.Fprintln(writer, proxy)
			if err != nil {
				log.Printf("❌ Gagal menulis proxy ke file: %v", err)
			} else {
				count++
			}
		}
	}

	// Pastikan semua buffer tertulis ke file.
	writer.Flush()
	log.Printf("\n💾 Sebanyak %d proxy unik yang aktif berhasil disimpan.", count)
}
