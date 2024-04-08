# Desafio 07 - Observabilidade Open Telemetry + Zipkin

Para rodar a aplicação use o docker-compose com o comando abaixo:

```
docker-compose up -d
```

Para acessar a rota do servico, utilize alguem aplicativo para fazer um `POST` no seguinte endereço:

```
http://localhost:8080
```

Use um payload `JSON` com um CEP válido como no exemplo abaixo:

```
{
  "cep": "45208643"
}
```

Para acessar a telemetria use o seguinte endereço do zapkin e após realizar uma requisi
ção clique no botão "RUN QUERY":

```
http://localhost:9411/zipkin
```
