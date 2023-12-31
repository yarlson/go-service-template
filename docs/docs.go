// Code generated by swaggo/swag. DO NOT EDIT.

package docs

import "github.com/swaggo/swag/v2"

const docTemplate = `{
    "schemes": {{ marshal .Schemes }},
    "components": {},
    "info": {"contact":{"email":"support@swagger.io","name":"API Support","url":"http://www.swagger.io/support"},"description":"{{escape .Description}}","license":{"name":"Apache 2.0","url":"http://www.apache.org/licenses/LICENSE-2.0.html"},"termsOfService":"http://swagger.io/terms/","title":"{{.Title}}","version":"{{.Version}}"},
    "externalDocs": {"description":"","url":""},
    "paths": {},
    "openapi": "3.1.0",
    "servers": [
        {"description":"http","url":"http://localhost"},
        {"description":"https","url":"https://localhost"}
    ]
}`

// SwaggerInfo holds exported Swagger Info so clients can modify it
var SwaggerInfo = &swag.Spec{
	Version:          "1.0",
	Title:            "Go Service Template API",
	Description:      "This is a sample server for a Go service template.",
	InfoInstanceName: "swagger",
	SwaggerTemplate:  docTemplate,
	LeftDelim:        "{{",
	RightDelim:       "}}",
}

func init() {
	swag.Register(SwaggerInfo.InstanceName(), SwaggerInfo)
}
