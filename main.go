package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"math/bits"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	gq "github.com/PuerkitoBio/goquery"
)

const (
	URL                    = "https://winworldpc.com"
	csvFileName            = "results.csv"
	csvFullResultsFileName = "results_full.csv"
	logFileName            = "output.log"
	UserAgent              = "Mozilla/5.0 (compatible; IPFS.ScraperBot/v1.1; +https://github.com/Neo-Desktop/winworldpc-ipfs-scraper)"
)

type File struct {
	Name         string
	Version      string
	Language     string
	GUID         string
	Size         string
	Hash         string
	Architecture string
	IPFSLink     string
	MirrorLinks  []string
}

func (f File) MarshalCSV() []string {
	out := []string{
		f.Name,
		f.Version,
		f.Language,
		f.GUID,
		f.Size,
		f.Hash,
		f.Architecture,
		f.IPFSLink,
	}

	out = append(out, f.MirrorLinks...)
	return out
}

type Article struct {
	Title    string
	Version  string
	WWPCLink string
	Files    []File
}

func scrapeSearchPageForUpperBound() uint {
	urlA, err := url.Parse(URL + "/search")
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Fetching search paginaion upper bound")

	res, err := fetch(urlA.String())
	if err != nil {
		log.Fatal(err)
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Fatalf("status code error: %d %s", res.StatusCode, res.Status)
	}

	doc, err := gq.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal(err)
	}

	lastPage := doc.Find("#searchPagination>li").Last()
	lastPageText := strings.TrimSpace(lastPage.Text())

	out, err := strconv.ParseUint(lastPageText, 10, bits.UintSize)
	if err != nil {
		log.Fatalf("unable to parse search pagination upper bound")
	}

	log.Printf("====== Found %d total pages ======", out)

	return uint(out)
}

func scrapeSearchPage(page uint) []Article {
	log.Printf("=============================== PAGE %2d ===============================", page)
	urlA, err := url.Parse(URL + "/search")
	if err != nil {
		log.Fatal(err)
	}

	queryParameters := urlA.Query()
	queryParameters.Add("sort", "most-recent")
	queryParameters.Add("page", fmt.Sprintf("%d", page))
	urlA.RawQuery = queryParameters.Encode()

	res, err := fetch(urlA.String())
	if err != nil {
		log.Fatal(err)
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Fatalf("status code error: %d %s", res.StatusCode, res.Status)
	}

	doc, err := gq.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal(err)
	}

	articles := make([]Article, 0)

	doc.Find(".media>.media-body").Each(func(i int, s *gq.Selection) {
		title := strings.TrimSpace(s.Find(".mt-0 a").First().Text())

		s.Find(".nav>.nav-link>a").Each(func(j int, s1 *gq.Selection) {
			url, ok := s1.Attr("href")
			if !ok {
				log.Printf("anchor does not have a href")
				return
			}

			version := strings.TrimSpace(s1.Text())

			articles = append(articles, scrapeArticlePage(Article{
				Title:    title,
				Version:  version,
				WWPCLink: url,
			}))
		})
	})

	return articles
}

func scrapeArticlePage(article Article) Article {
	res, err := fetch(URL + article.WWPCLink)
	if err != nil {
		log.Println(err)
		return article
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Printf("status code error: %d %s", res.StatusCode, res.Status)
		return article
	}

	doc, err := gq.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Println(err)
		return article
	}

	doc.Find("#downloadsTable tbody tr").Each(func(i int, tr *gq.Selection) {
		file := File{}

		tr.Find("td").Each(func(j int, td *gq.Selection) {
			switch j {

			case 0: // download name / link / guid
				link, ok := td.Find("a").Attr("href")
				if ok {
					file.GUID = strings.Replace(link, "/download/", "", -1)
				}
				file.Name = strings.TrimSpace(td.Text())

			case 1: // version
				file.Version = strings.TrimSpace(td.Text())

			case 2: // language
				file.Language = strings.TrimSpace(td.Text())

			case 3: // architecture
				title, ok := td.Find("img").Attr("title")
				if ok {
					file.Architecture = title
				}

			case 4: // file size / hash
				file.Size = strings.TrimSpace(td.Text())
				hash, ok := td.Find("span").Attr("title")
				if ok {
					file.Hash = strings.TrimSpace(hash)
				}

				//case 5: // download counter
			}
		})

		article.Files = append(article.Files, scrapeDownloadPage(file))
	})

	return article
}

func scrapeDownloadPage(file File) File {
	res, err := fetch(URL + "/download/" + file.GUID)
	if err != nil {
		log.Println(err)
		return file
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Printf("status code error: %d %s", res.StatusCode, res.Status)
		return file
	}

	doc, err := gq.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Println(err)
		return file
	}

	link, ok := doc.Find("#localClientLink a").Attr("href")
	if ok {
		file.IPFSLink = link
	}

	doc.Find("#mirrorsList a").Each(func(i int, s *gq.Selection) {
		link, ok := s.Attr("href")
		if ok {
			file.MirrorLinks = append(file.MirrorLinks, link)
		}
	})

	return file
}

func fetch(urlA string) (*http.Response, error) {
	client := &http.Client{}

	log.Printf("sleeping 3 seconds before requesting %s", urlA)
	time.Sleep(3 * time.Second)

	req, err := http.NewRequest(http.MethodGet, urlA, nil)
	if err != nil {
		return req.Response, err
	}

	req.Header.Set("User-Agent", UserAgent)
	return client.Do(req)
}

func writeCSV(articles []Article, filename string) {
	csvFile, err := os.OpenFile(filename, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		log.Println(err)
		return
	}

	defer csvFile.Close()

	csvWriter := csv.NewWriter(csvFile)
	defer csvWriter.Flush()

	for _, article := range articles {
		for _, file := range article.Files {
			_ = csvWriter.Write(file.MarshalCSV())
		}
	}
}

func main() {
	logHandle, err := os.OpenFile(logFileName, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}

	defer logHandle.Close()

	logWriter := io.MultiWriter(os.Stdout, logHandle)
	log.SetOutput(logWriter)

	log.Printf("WinWorld IPFS Scraper Started")
	startTime := time.Now()

	articles := make([]Article, 0)

	pageUpperBound := scrapeSearchPageForUpperBound()

	for i := uint(1); i < pageUpperBound; i++ {
		results := scrapeSearchPage(i)
		articles = append(articles, results...)
		go writeCSV(results, csvFileName)
	}

	writeCSV(articles, csvFullResultsFileName)

	log.Printf("WinWorld IPFS Scraper completed")
	log.Printf("Total time: %s", time.Now().Sub(startTime).String())
}
