package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

type Cep struct {
	Cep string `json:"cep"`
}

type TemperatureResponse struct {
	City  string  `json:"city"`
	TempC float64 `json:"temp_C"`
	TempF float64 `json:"temp_F"`
	TempK float64 `json:"temp_K"`
}

func initTracer() {
	exporter, err := zipkin.New("http://zipkin:9411/api/v2/spans")
	if err != nil {
		log.Fatalf("Fail to create Zipkin exporter: %v", err)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("service-a"),
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
}

func main() {
	initTracer()

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	r.Post("/", handleRequest)

	fmt.Println("Server running on port 8080")
	http.ListenAndServe(":8080", r)
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	// carrier := propagation.HeaderCarrier(r.Header)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid zipcode", http.StatusUnprocessableEntity)
		return
	}

	var cep Cep
	err = json.Unmarshal(body, &cep)
	if err != nil {
		http.Error(w, "invalid zipcode", http.StatusUnprocessableEntity)
		return
	}

	ctx, span := otel.Tracer("service-a").Start(r.Context(), "validate-cep")
	span.SetAttributes(attribute.String("cep", cep.Cep))
	defer span.End()

	if !isValidZipcode(cep.Cep) {
		http.Error(w, "invalid zipcode", http.StatusUnprocessableEntity)
		return
	}

	temperature, status, err := getTemperature(cep.Cep, ctx)
	if err != nil {
		http.Error(w, "invalid zipcode", status)
		return
	}

	jsonData, err := json.Marshal(temperature)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Write([]byte(jsonData))
}

// Call service B
func getTemperature(cep string, ctx context.Context) (*TemperatureResponse, int, error) {
	_, span := otel.Tracer("service-a").Start(ctx, "request-service-b")
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://goapp-service-b:8081/"+cep, nil)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}

	var temperatureResponse TemperatureResponse
	err = json.Unmarshal(body, &temperatureResponse)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}

	return &temperatureResponse, http.StatusOK, nil
}

func isValidZipcode(zipcode string) bool {
	if zipcode == "" || len(zipcode) != 8 {
		return false
	}

	for _, char := range zipcode {
		if _, err := strconv.Atoi(string(char)); err != nil {
			return false
		}
	}

	return true
}
