# Go Service Template

This service template written in Go represents a scalable and maintainable architecture that is mindful of separations of concerns, layered design, and coherent organization. The codebase has been carefully designed to be both easily understood and modified, with a layout that reflects the system's architecture and dependencies.

## Architecture Overview

### Core Server
The core server is constructed with a focus on modular design, where each package serves a well-defined purpose. The server consists of an HTTP server to handle requests, equipped with a router that defines routes and includes relevant middleware such as logging and JWT authentication.

### Metrics Server
A separate metrics server serves Prometheus metrics, allowing for easy observability and monitoring.

### Configuration Management
The service configuration is handled through environment variables, which can be set through a `.env` file or the host system. This enables a flexible and secure way to manage configuration without hardcoding values.

### Logging
Logrus, a popular structured logger for Go, is employed for consistent logging across the application.

### Middleware
Various middleware functions are used to manage concerns like logging, JWT authentication, and metrics collection.

## How to Use

1. **Configuration**: Set up the necessary environment variables. More on this in the Environment Variables section below.
2. **Build**: Compile the Go code using `go build -o my_service ./cmd/main.go`.
3. **Run**: Execute the binary `./my_service`.

## Environment Variables

The following table describes the environment variables that the system uses:

| Variable                 | Description                                              | Default          | Required |
|--------------------------|----------------------------------------------------------|------------------|----------|
| `APP_PORT`               | Port for the HTTP server to listen on                    | 3000             | No       |
| `APP_BIND_ADDRESS`       | Bind address for the HTTP server                         | "0.0.0.0"        | No       |
| `JWT_PUBLIC_KEY`         | Public key for JWT authentication                        | -                | Yes      |
| `LOG_LEVEL`              | Logging level (e.g. "debug", "info", "warn", "error")    | -                | Yes      |
| `METRICS_PORT`           | Port for the metrics server to listen on                 | 2112             | No       |
| `METRICS_BIND_ADDRESS`   | Bind address for the metrics server                      | "0.0.0.0"        | No       |
| `METRICS_ENABLED`        | Flag to enable or disable the metrics server             | true             | No       |
| `DATABASE_URL`           | URL to the database                                      | -                | Yes      |
| `REDIS_HOST`             | Redis host                                               | -                | Yes      |
| `REDIS_DB`               | Redis database index                                     | -                | Yes      |
| `REDIS_PORT`             | Redis port                                               | -                | Yes      |
| `REDIS_USERNAME`         | Redis username                                           | ""               | No       |
| `REDIS_PASSWORD`         | Redis password                                           | ""               | No       |
| `REDIS_TLS_ENABLED`      | Flag to enable or disable TLS for Redis                  | true             | No       |
| `REDIS_COMMAND_TIMEOUT`  | Redis command timeout                                    | 0                | No       |
| `REDIS_CONNECT_TIMEOUT`  | Redis connect timeout                                    | 0                | No       |

Additional settings for the application, such as the application version or the specific range for the Redis database index, are also configurable.

For a more detailed explanation and the corresponding code files, please refer to the source code of the project.

## Conclusion

This Go service template is an example of how thoughtful architecture design can lead to a maintainable and scalable codebase. By following the principles of separation of concerns and leveraging well-known libraries, the template provides a solid foundation for building robust services in Go.
