package plugin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/hbh/beds2/pkg/models"
)

// Make sure Datasource implements required interfaces. This is important to do
// since otherwise we will only get a not implemented error response from plugin in
// runtime. In this example datasource instance implements backend.QueryDataHandler,
// backend.CheckHealthHandler interfaces. Plugin should not implement all these
// interfaces - only those which are required for a particular task.
var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// NewDatasource creates a new datasource instance.
func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	log.Println("HBH: in NewDatasource")
	log.Println("HBH: settings", settings)
	return &Datasource{
		baseURL:  settings.URL,
		user:     settings.BasicAuthUser,
		password: settings.DecryptedSecureJSONData["basicAuthPassword"],
	}, nil
}

// Datasource is an example datasource which can respond to data queries, reports
// its health and has streaming skills.
type Datasource struct {
	baseURL  string
	user     string
	password string
}

// Dispose here tells plugin SDK that plugin wants to clean up resources when a new instance
// created. As soon as datasource settings change detected by SDK old datasource instance will
// be disposed and a new one will be created using NewSampleDatasource factory function.
func (d *Datasource) Dispose() {
	// Clean up datasource instance resources.
}

// QueryData handles multiple queries and returns multiple responses.
// req contains the queries []DataQuery (where each query contains RefID as a unique identifier).
// The QueryDataResponse contains a map of RefID to the response for each query, and each response
// contains Frames ([]*Frame).
func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	// create response struct
	response := backend.NewQueryDataResponse()
	log.Println("HBH: in QueryData")
	log.Println("HBH: in QueryData: baseURL: ", d.baseURL)
	log.Println("HBH: in QueryData: user: ", d.user)
	log.Println("HBH: in QueryData: password: ", d.password)
	// loop over queries and execute them individually.
	for _, q := range req.Queries {
		res := d.query(ctx, req.PluginContext, q)

		// save the response in a hashmap
		// based on with RefID as identifier
		response.Responses[q.RefID] = res
	}

	return response, nil
}

type queryModel struct {
	RefId     string `json:"refId"`
	QueryText string `json:"queryText"`
}

// TodosAPIResponse represents the structure of the JSONPlaceholder response.
/* type TodosAPIResponse struct {
	UserID    int     `json:"userId"`
	ID        float32 `json:"id"`
	Title     string  `json:"title"`
	Completed bool    `json:"completed"`
} */

type SplunkResponse struct {
	Sid     string                   `json:"sid"`
	Entry   []Entry                  `json:"entry"`
	Fields  []map[string]string      `json:"fields"`
	Results []map[string]interface{} `json:"results"`
}

type Entry struct {
	Content Content `json:"content"`
}

type Content struct {
	DispatchState string `json:"dispatchState"`
}

func (d *Datasource) query(_ context.Context, pCtx backend.PluginContext, query backend.DataQuery) backend.DataResponse {
	var response backend.DataResponse

	// Unmarshal the JSON into our queryModel. what for?
	var qm queryModel

	log.Println("HBH: in query")
	log.Println("HBH: query.JSON", query.JSON)
	jsonString := string(query.JSON)
	log.Println("HBH: query.JSON", jsonString)

	err := json.Unmarshal(query.JSON, &qm)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err.Error()))
	}
	log.Println("HBH: query model", qm)

	// query the api
	sid := ""
	sid, err = d.getSearchRequestSid(qm.QueryText)
	if err != nil {
		log.Printf("HBH: failed to get sid: %v\n", err)
		panic(err)
	}
	log.Println("HBH: sid:", sid)
	// Now loop and wait for request result to be ready
	for {
		status, err := d.doSearchStatusRequest(sid)
		if err != nil {
			// call resulted in error
			log.Printf("HBH: doSearchStatusRequest error: %v\n", err)
			panic(err)
		}

		if status {
			break // Exit when status is ready
		}
		log.Println("HBH: Waiting for search to complete...")
		time.Sleep(100 * time.Millisecond) // Delay to prevent excessive requests
	}
	log.Println("HBH:Search completed!")

	// Now get the results
	fields, results, err := d.doGetAllResultsRequest(sid)
	if err != nil {
		log.Println("HBH: doGetAllResultsRequest Error:", err)
		panic(err)
	}

	log.Println("HBH: Fields:", fields)
	log.Println("HBH: Results:", results)

	/* resp, err := http.Get("https://jsonplaceholder.typicode.com/todos")
	if err != nil {
		log.Printf("HBH: failed to fetch data: %v\n", err)
		panic(err)
	}
	defer resp.Body.Close()

	log.Println("HBH: b4 todos")
	var todos []TodosAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&todos); err != nil {
		log.Printf("HBH:failed to parse response: %v\n", err)
		panic(err)
	}
	log.Printf("HBH: todos: %v\n", todos)
	*/
	// log.Println("HBH: b4 frame")

	frame := data.NewFrame(qm.RefId)

	// Initialize fields in the DataFrame
	for _, fieldName := range fields {
		log.Println("HBH: fielName", fieldName)
		values := make([]string, len(results)) // Allocate space for values
		frame.Fields = append(frame.Fields, data.NewField(fieldName, nil, values))
	}

	/// Populate field values
	for i, result := range results {
		// log.Println("HBH: i:", i)
		for j, field := range frame.Fields {
			// log.Println("HBH: j:", j)
			if value, exists := result[field.Name]; exists {
				// log.Printf("HBH: fieldName: %v : value: %v", field.Name, value)
				stringValue := fmt.Sprintf("%s", value)
				frame.Fields[j].Set(i, stringValue) // Correct way to set values in Grafana SDK
			} else {
				// log.Printf("HBH: fieldName: %v : no value!", field.Name)
				frame.Fields[j].Set(i, nil) // Handle missing data gracefully
			}
		}
	}

	// log.Println("HBH: after frame")

	/* for _, todo := range todos {
		// log.Println("HBH: inside todos loop")
		frame.AppendRow(todo.ID, todo.Title, todo.Completed)
	} */
	// log.Printf("HBH: DataFrame %v\n", frame)

	response.Frames = append(response.Frames, frame)

	return response
}

// Get the Splunk request sid
func (d *Datasource) getSearchRequestSid(searchString string) (string, error) {
	// Splunk API endpoint
	splunkURL := d.baseURL
	// splunkURL := "https://splunk:8089/services/search/jobs"
	log.Printf("HBH: splunkURL", splunkURL)

	// Splunk credentials
	username := d.user
	password := d.password

	// Prepare authentication header (Basic Auth)
	auth := username + ":" + password
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))

	// data := "search=search sourcetype=access_combined&output_mode=json"
	data := "search=search " + searchString + "&output_mode=json"
	log.Printf("HBH: data %v\n", data)

	jsonData := []byte(data)
	// Prepare HTTP request
	req, err := http.NewRequest("POST", splunkURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("HBH: Error creating request:", err)
		return "", err
	}

	// Set headers
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Send the request
	// Disable certificate verification
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	// client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("HBH: Error sending request:", err)
		return "", err
	}
	defer resp.Body.Close()

	//get the sid

	var result SplunkResponse
	sid := ""
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("HBH:failed to parse response: %v\n", err)
		return "", err
	} else {
		sid = result.Sid
		log.Println("sid:", sid)
		return sid, nil
	}

}

func (d *Datasource) doSearchStatusRequest(sid string) (bool, error) {
	// Splunk API endpoint
	// splunkURL := "https://localhost:8089/services/search/jobs/" + sid

	splunkURL := d.baseURL + "/" + sid
	log.Printf("HBH: splunkURL", splunkURL)

	// Splunk credentials
	username := d.user
	password := d.password

	// Prepare authentication header (Basic Auth)
	auth := username + ":" + password
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))

	params := url.Values{}
	params.Add("output_mode", "json")

	splunkURL = splunkURL + "?" + params.Encode()
	// Create request
	req, err := http.NewRequest("GET", splunkURL, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return false, err
	}

	// Optional: Add headers
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	// Disable certificate verification
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	// client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return false, err
	}
	defer resp.Body.Close()
	//get the status
	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return false, err
	} // after this, resp.Body is empty!

	jsonString := string(body)
	fmt.Println("Response:", jsonString)

	// get dispatchState
	// Unmarshal JSON
	var result SplunkResponse
	err = json.Unmarshal([]byte(jsonString), &result)
	if err != nil {
		fmt.Println("Error parsing JSON:", err)
		return false, err
	}

	// Extract dispatchState
	status := ""
	if len(result.Entry) > 0 {
		status = result.Entry[0].Content.DispatchState
		fmt.Println("Dispatch State:", result.Entry[0].Content.DispatchState)
		return status == "DONE" || status == "PAUSED" || status == "FAILED", nil
	} else {
		fmt.Println("No entries found.")
		return false, errors.New("No entries found.")
	}

}

// Function to fetch results from Splunk API
func (d *Datasource) doGetAllResultsRequest(sid string) ([]string, []map[string]interface{}, error) {
	baseURL := d.baseURL + "/" + sid + "/results"
	log.Printf("HBH: baseURL", baseURL)

	// baseURL := "https://localhost:8089/services/search/jobs/" + sid + "/results"
	count := 50000
	offset := 0
	isFirst := true
	isFinished := false
	var fields []string
	var results []map[string]interface{}

	// Splunk credentials
	username := d.user
	password := d.password

	// Prepare authentication header (Basic Auth)
	auth := username + ":" + password
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
	iteration := 0

	for !isFinished {
		// Prepare query parameters
		params := url.Values{}
		params.Set("output_mode", "json")
		params.Set("offset", fmt.Sprintf("%d", offset))
		params.Set("count", fmt.Sprintf("%d", count))

		// Make GET request
		/* resp, err := http.Get(splunkURL + "?" + params.Encode())
		if err != nil {
			return nil, nil, fmt.Errorf("error sending request: %v", err)
		} */

		//
		splunkURL := baseURL + "?" + params.Encode()
		// Create request
		req, err := http.NewRequest("GET", splunkURL, nil)
		if err != nil {
			fmt.Println("Error creating request:", err)
			return nil, nil, err
		}

		// Optional: Add headers
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Accept", "application/json")

		// Disable certificate verification
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		// client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("Error sending request:", err)
			return nil, nil, err
		}
		//
		defer resp.Body.Close()

		// Read response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("error reading response: %v", err)
		}

		// Parse JSON response
		var data SplunkResponse
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, nil, fmt.Errorf("error decoding JSON: %v", err)
		}

		// Logging equivalent of console.log
		fmt.Println("Iteration:", iteration)
		iteration++

		// Check if results are empty
		if len(data.Results) == 0 {
			isFinished = true
		} else {
			if isFirst {
				isFirst = false
				for _, field := range data.Fields {
					fields = append(fields, field["name"])
				}
			}
			results = append(results, data.Results...)
			offset += count
		}
	}

	// Handle `_time` and `_raw` fields
	/* if contains(fields, "_time") {
		fields = append(fields, "Time")
	}

	fields = reorderFields(fields, "_raw") */

	return fields, results, nil
}

// Helper function to check if a slice contains a value
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// Helper function to reorder `_raw` field
func reorderFields(fields []string, target string) []string {
	index := -1
	for i, f := range fields {
		if f == target {
			index = i
			break
		}
	}
	if index != -1 {
		fields = append(fields[:index], fields[index+1:]...)
		fields = append([]string{target}, fields...)
	}
	return fields
}

// CheckHealth handles health checks sent from Grafana to the plugin.
// The main use case for these health checks is the test button on the
// datasource configuration page which allows users to verify that
// a datasource is working as expected.
func (d *Datasource) CheckHealth(_ context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	res := &backend.CheckHealthResult{}
	config, err := models.LoadPluginSettings(*req.PluginContext.DataSourceInstanceSettings)

	log.Println("HBH: in CheckHealth")

	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = "Unable to load settings"
		return res, nil
	}

	if config.Secrets.ApiKey == "" {
		res.Status = backend.HealthStatusError
		res.Message = "API key is missing"
		return res, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Data source is working",
	}, nil
}
