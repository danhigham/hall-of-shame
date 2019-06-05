package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"code.cloudfoundry.org/cli/plugin"

	"github.com/olekukonko/tablewriter"
	"github.com/remeh/sizedwaitgroup"
	pb "gopkg.in/cheggaaa/pb.v1"
)

type statTime struct {
	time.Time
}

type appStatSummary struct {
	Name         string
	GUID         string
	Space        string
	Instances    int
	MemoryAlloc  int
	AvgMemoryUse int
	Ratio        float64
}

type byRatio []appStatSummary

func (s *appStatSummary) toValueList() []string {
	return []string{s.Name, s.Space, fmt.Sprintf("%d", s.MemoryAlloc), fmt.Sprintf("%d", s.AvgMemoryUse), fmt.Sprintf("%f", s.Ratio)}
}

func (a byRatio) Len() int           { return len(a) }
func (a byRatio) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byRatio) Less(i, j int) bool { return a[j].Ratio < a[i].Ratio }

type AppSearchResults struct {
	Resources []*AppSearchResoures `json:"resources"`
}

type AppSearchResoures struct {
	Metadata *AppSearchMetaData `json:"metadata"`
	Entity   *AppSearchEntity   `json:"entity"`
}

type AppSearchMetaData struct {
	Guid string `json:"guid"`
	Url  string `json:"url"`
}

type AppSearchEntity struct {
	Name      string `json:"name"`
	Instances int    `json:"instances"`
	SpaceGuid string `json:"space_guid"`
}

type AppStat struct {
	State        string `json:"state"`
	IsolationSeg string `json:"isolation_segment"`
	Stats        struct {
		Name      string   `json:"name"`
		Uris      []string `json:"uris"`
		Host      string   `json:"host"`
		Port      int      `json:"port"`
		Uptime    int      `json:"uptime"`
		MemQuota  int      `json:"mem_quota"`
		DiskQuota int      `json:"disk_quota"`
		FdsQuota  int      `json:"fds_quota"`
		Usage     struct {
			Time statTime `json:"time"`
			CPU  float64  `json:"cpu"`
			Mem  int      `json:"mem"`
			Disk int      `json:"disk"`
		} `json:"usage"`
	} `json:"stats"`
}

type HallOfShame struct{}

func (hallOfShame *HallOfShame) Run(cliConnection plugin.CliConnection, args []string) {

	var appStats []appStatSummary

	res, err := hallOfShame.GetAllApps(cliConnection)
	if err != nil {
		panic(err)
	}

	bar := pb.StartNew(len(res.Resources))

	wg := sizedwaitgroup.New(2)
	for _, app := range res.Resources {

		wg.Add()

		go func(cfApp *AppSearchResoures, pb *pb.ProgressBar) {
			defer wg.Done()

			stats, err := hallOfShame.GetAppStats(cliConnection, cfApp.Metadata.Guid)
			pb.Increment()

			if err != nil {
				return
			}

			if stats["0"].State != "RUNNING" {
				return
			}

			memAlloc := stats["0"].Stats.MemQuota

			var totalUsage int
			for _, stat := range stats {
				totalUsage += stat.Stats.Usage.Mem
			}

			stat := appStatSummary{
				Name:         cfApp.Entity.Name,
				GUID:         cfApp.Metadata.Guid,
				Instances:    cfApp.Entity.Instances,
				MemoryAlloc:  memAlloc,
				Space:        cfApp.Entity.SpaceGuid,
				AvgMemoryUse: totalUsage / len(stats),
				Ratio:        float64(memAlloc) / float64(totalUsage/len(stats)),
			}

			appStats = append(appStats, stat)

		}(app, bar)

	}

	wg.Wait()

	bar.FinishPrint("Done!")

	sort.Sort(byRatio(appStats))

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Name", "Space", "Alloc", "AvgUse", "Ratio"})

	for _, v := range appStats {
		table.Append(v.toValueList())
	}

	table.Render()

}

func (hallOfShame *HallOfShame) GetAppStats(cliConnection plugin.CliConnection, appGuid string) (map[string]AppStat, error) {

	appQuery := fmt.Sprintf("/v2/apps/%v/stats", appGuid)
	cmd := []string{"curl", appQuery}

	output, _ := cliConnection.CliCommandWithoutTerminalOutput(cmd...)

	buffer := new(bytes.Buffer)
	if err := json.Compact(buffer, []byte(strings.Join(output, ""))); err != nil {
		fmt.Println(err)
	}

	// fmt.Printf("********\n%s\n\n%+v\n*********\n\n", appQuery, buffer)
	statResult := map[string]AppStat{}
	err := json.Unmarshal([]byte(strings.Join(output, "")), &statResult)

	return statResult, err
}

func (hallOfShame *HallOfShame) GetAllApps(cliConnection plugin.CliConnection) (AppSearchResults, error) {

	appQuery := fmt.Sprintf("/v2/apps")
	cmd := []string{"curl", appQuery}

	output, _ := cliConnection.CliCommandWithoutTerminalOutput(cmd...)
	res := AppSearchResults{}
	json.Unmarshal([]byte(strings.Join(output, "")), &res)

	return res, nil
}

func (hallOfShame *HallOfShame) GetMetadata() plugin.PluginMetadata {
	return plugin.PluginMetadata{
		Name: "HallOfShame",
		Version: plugin.VersionType{
			Major: 0,
			Minor: 1,
			Build: 1,
		},
		Commands: []plugin.Command{
			{
				Name:     "Memory Hall of Shame",
				Alias:    "hall-of-shame",
				HelpText: "Reviews memory usages by  orgs and space. To obtain more information use --help",
				UsageDetails: plugin.Usage{
					Usage: "hall-of-shame - list memory in use by org and space.\n   cf memshame [-org] [-space]",
					Options: map[string]string{
						"org":   "Specify the org to report",
						"space": "Specify the space to report (requires -org)",
					},
				},
			},
		},
	}
}

func main() {
	plugin.Start(new(HallOfShame))
}
