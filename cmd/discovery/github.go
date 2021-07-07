package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
)

// var login, password, repo, tag, token string
// var assets, download bool

const (
	tfrGHRepo        = "GoogleCloudPlatform/terraformer"
	tfProviderGHRepo = "IBM-Cloud/terraform-provider-ibm"
	tfUrl            = "https://releases.hashicorp.com/terraform"
	login            = ""
	pwd              = ""
	token            = ""
)

func prepareRequest(url string) *http.Request {

	req, _ := http.NewRequest("GET", url, nil)
	if len(login)+len(pwd) > 0 {
		req.SetBasicAuth(login, pwd)
	} else if len(token) > 0 {
		req.Header.Add("Authorization", fmt.Sprintf("token %s", token))
	}
	req.Header.Add("User-Agent", "metal3d-go-client")
	return req
}

// Download resource from given url, write 1 in chan when finished
func downloadResource(repository string, id float64, c chan int) {
	defer func() { c <- 1 }()
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/assets/%.0f", repository, id)
	fmt.Printf("Start: %s\n", url)
	req := prepareRequest(url)

	req.Header.Add("Accept", "application/octet-stream")

	client := http.Client{}
	resp, _ := client.Do(req)

	disp := resp.Header.Get("Content-disposition")
	re := regexp.MustCompile(`filename=(.+)`)
	matches := re.FindAllStringSubmatch(disp, -1)

	if len(matches) == 0 || len(matches[0]) == 0 {
		log.Println("WTF: ", matches)
		log.Println(resp.Header)
		log.Println(req)
		return
	}

	disp = matches[0][1]

	f, err := os.OpenFile(disp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0664)
	if err != nil {
		log.Fatal(err)
	}

	b := make([]byte, 4096)
	var i int

	for err == nil {
		i, err = resp.Body.Read(b)
		f.Write(b[:i])
	}
	fmt.Printf("Finished: %s -> %s\n", url, disp)
	f.Close()
}

// flag.StringVar(&login, "login", "", "Github Login")
// flag.StringVar(&password, "password", "", "Github Password")
// flag.StringVar(&repo, "repo", "", "Github repository (eg. yourname/project1)")
// flag.BoolVar(&assets, "assets", false, "Print assets urls instead of source download link")
// flag.StringVar(&tag, "tag", "", "Get this release tag instead of latest")
// flag.StringVar(&token, "token", "", "Use OAUTH2 token")
// flag.BoolVar(&download, "dl", false, "Download results in current directory")
// flag.Parse()
func downloadTar(versionTag, repo, folder, tag string, assets, download bool) error {

	if len(repo) == 0 {
		log.Fatal("No repository provided")
	}

	// command to call
	command := "releases/latest"
	if len(tag) > 0 {
		command = fmt.Sprintf("releases/tags/%s", tag)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", repo, command)

	// create a request with basic-auth
	req := prepareRequest(url)

	// Add required headers
	req.Header.Add("Accept", "application/vnd.github.v3.text-match+json")
	req.Header.Add("Accept", "application/vnd.github.moondragon+json")

	// call github
	client := http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		log.Fatal("Error while making request", err)
	}

	// status in <200 or >299
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		log.Fatalf("Error: %d %s", resp.StatusCode, resp.Status)
	}

	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Error reading response", err)
	}

	// prepare result
	result := make(map[string]interface{})
	json.Unmarshal(bodyText, &result)

	// print download url
	results := make([]interface{}, 0)
	if !assets {
		if download {
			results = append(results, result["id"])
		} else {
			results = append(results, result["zipball_url"])
		}
	} else {
		for _, asset := range result["assets"].([]interface{}) {
			if download {
				results = append(results, asset.(map[string]interface{})["id"])
			} else {
				results = append(results, asset.(map[string]interface{})["browser_download_url"])
			}
		}
	}

	if !download {
		// only print results
		for _, res := range results {
			fmt.Println(res)
		}
	} else {
		// Download results - parallel downloading, use channel to syncronize
		c := make(chan int)
		for _, res := range results {
			go downloadResource(repo, res.(float64), c)
		}
		// wait for downloads end
		for i := 0; i < len(results); i++ {
			<-c
		}
	}
	return nil
}
