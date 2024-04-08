package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

type AddressResponse struct {
	CEP         string `json:"cep,omitempty"`
	Logradouro  string `json:"logradouro,omitempty"`
	Complemento string `json:"complemento,omitempty"`
	Bairro      string `json:"bairro,omitempty"`
	Localidade  string `json:"localidade,omitempty"`
	UF          string `json:"uf,omitempty"`
	Erro        bool   `json:"erro,omitempty"`
}

type WeatherResponse struct {
	Location struct {
		Name string `json:"name"`
	} `json:"location"`
	Current struct {
		TempC float64 `json:"temp_c"`
	} `json:"current"`
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
			semconv.ServiceNameKey.String("service-b"),
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
}

func main() {
	initTracer()

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/{cep}", func(r chi.Router) {
		r.Use(checkCepMiddleware)
		r.Get("/", handleGetTemperatureByCEP)
	})

	fmt.Println("Server running on port 8081")
	http.ListenAndServe(":8081", r)
}

func checkCepMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cep := chi.URLParam(r, "cep")

		if cep == "" || len(cep) == 0 {
			http.Error(w, "CEP is required", http.StatusBadRequest)
			return
		}

		if !isValidZipcode(cep) {
			http.Error(w, "invalid zipcode", http.StatusUnprocessableEntity)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func getAddressFromViaCEP(cep string, ctx context.Context) (*AddressResponse, error) {
	_, span := otel.Tracer("service-b").Start(ctx, "get-cep-location")
	defer span.End()

	url := fmt.Sprintf("https://viacep.com.br/ws/%s/json/", cep)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var address AddressResponse
	err = json.NewDecoder(resp.Body).Decode(&address)
	if address.Erro {
		return nil, fmt.Errorf("zipcode not found")
	}
	if err != nil {
		return nil, err
	}
	return &address, nil
}

func getWeather(city string, ctx context.Context) (*WeatherResponse, error) {
	_, span := otel.Tracer("service-b").Start(ctx, "get-weather")
	defer span.End()

	cityEncoded := url.QueryEscape(city)
	url := fmt.Sprintf("https://api.weatherapi.com/v1/current.json?key=e1fece5bce574041a9f130048241703&q=%s&aqi=no", cityEncoded)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var weather WeatherResponse
	err = json.NewDecoder(resp.Body).Decode(&weather)
	if err != nil {
		return nil, err
	}

	return &weather, nil
}

func isValidZipcode(zipcode string) bool {
	if len(zipcode) != 8 {
		return false
	}

	for _, char := range zipcode {
		if _, err := strconv.Atoi(string(char)); err != nil {
			return false
		}
	}

	return true
}

func celsiusToFahrenheit(celsius float64) float64 {
	return celsius*1.8 + 32
}

func celsiusToKelvin(celsius float64) float64 {
	return celsius + 273
}

func handleGetTemperatureByCEP(w http.ResponseWriter, r *http.Request) {
	cep := chi.URLParam(r, "cep")

	ctx, span := otel.Tracer("service-b").Start(r.Context(), "get-cep-temperature")
	defer span.End()

	address, err := getAddressFromViaCEP(cep, ctx)
	if err != nil {
		http.Error(w, "can not find zipcode", http.StatusNotFound)
		return
	}

	weather, err := getWeather(address.Localidade, ctx)
	if err != nil {
		http.Error(w, "can not find weather", http.StatusNotFound)
		return
	}

	temperature := TemperatureResponse{
		City:  address.Localidade,
		TempC: weather.Current.TempC,
		TempF: celsiusToFahrenheit(weather.Current.TempC),
		TempK: celsiusToKelvin(weather.Current.TempC),
	}
	json.NewEncoder(w).Encode(temperature)
}
