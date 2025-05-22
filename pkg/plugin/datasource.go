package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

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

type queryModel struct{}

// TodosAPIResponse represents the structure of the JSONPlaceholder response.
type TodosAPIResponse struct {
	UserID    int     `json:"userId"`
	ID        float32 `json:"id"`
	Title     string  `json:"title"`
	Completed bool    `json:"completed"`
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
	resp, err := http.Get("https://jsonplaceholder.typicode.com/todos")
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

	log.Println("HBH: b4 frame")

	/* labels := data.Labels{
		"source": "jsonplaceholder",
		"type":   "todos",
	}
	frame := data.NewFrame("Todos",
		data.NewField("ID", labels, []int{}),
		data.NewField("Title", labels, []string{}),
		data.NewField("Completed", labels, []bool{}),
	) */
	frame := data.NewFrame("Todos")
	// add fields.
	frame.Fields = append(frame.Fields,
		data.NewField("ID", nil, []float32{}),
		data.NewField("Title", nil, []string{}),
		data.NewField("Completed", nil, []bool{}))
	/* frame.Fields = append(frame.Fields,
		data.NewField("ID", nil, []int{}),
		data.NewField("Title", nil, []string{}),
		data.NewField("Completed", nil, []bool{}),
	) */

	log.Println("HBH: after frame")

	for _, todo := range todos {
		// log.Println("HBH: inside todos loop")
		frame.AppendRow(todo.ID, todo.Title, todo.Completed)
	}
	log.Printf("HBH: DataFrame %v\n", frame)

	// create data frame response.
	// For an overview on data frames and how grafana handles them:
	// https://grafana.com/developers/plugin-tools/introduction/data-frames
	// frame := data.NewFrame("response")

	// hbh
	/* n := 100
	startTime := query.TimeRange.From
	endTime := query.TimeRange.To
	interval := endTime.Sub(startTime) / time.Duration(n-1)
	times := make([]time.Time, n)
	for i := 0; i < n; i++ {
		times[i] = startTime.Add(time.Duration(i) * interval)
	}

	cosValues := make([]float64, n)
	step := (2 * math.Pi) / float64(n-1) // Evenly space values between 0 and 2π

	for i := 0; i < n; i++ {
		x := float64(i) * step
		cosValues[i] = math.Cos(x)
	} */

	// add fields.
	/* frame.Fields = append(frame.Fields,
		data.NewField("time", nil, []time.Time{query.TimeRange.From, query.TimeRange.To}),
		data.NewField("values", nil, []int64{10, 20}),
	) */
	/* frame.Fields = append(frame.Fields,
		data.NewField("time", nil, times),
		data.NewField("values", nil, cosValues),
	) */

	// add the frames to the response.
	response.Frames = append(response.Frames, frame)

	return response
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
