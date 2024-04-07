package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
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

func initProvider() (func(context.Context) error, error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("service-a"),
		),
	)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*3)
	defer cancel()

	tracerExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint("otel-collector:4317"),
	)
	if err != nil {
		return nil, err
	}

	bsp := sdktrace.NewBatchSpanProcessor(tracerExporter)

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tracerProvider.Shutdown, nil
}

func main() {
	ctx := context.Background()

	shutdown, err := initProvider()
	if err != nil {
		fmt.Println("Failed to initialize provider")
		return
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			fmt.Println("Failed to shutdown provider")
		}
	}()

	tracer := otel.Tracer("service-a")

	ctx, span := tracer.Start(context.Background(), "span-service-a")
	defer span.End()

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
	carrier := propagation.HeaderCarrier(r.Header)
	ctx := r.Context()
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
	ctx, span := otel.Tracer("service-a").Start(ctx, "handleRequest")
	defer span.End()

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
	req, err := http.NewRequestWithContext(ctx, "GET", "http://goapp-service-b:8081/"+cep, nil)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
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
