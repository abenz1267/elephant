// Package util provides general utility.
package util

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/abenz1267/elephant/v2/internal/providers"
	"github.com/abenz1267/elephant/v2/pkg/common"
)

func GenerateDoc(provider string) {
	provider = strings.ToLower(provider)
	
	if provider == "" || provider == "elephant" {
		fmt.Println("# Elephant")

		fmt.Println("A service providing various datasources which can be triggered to perform actions.")
		fmt.Println()
		fmt.Println("Run `elephant -h` to get an overview of the available commandline flags and actions.")

		fmt.Println("## Elephant Configuration")

		PrintConfig(common.ElephantConfig{}, "elephant")
	}

	if provider == "" {
		fmt.Println("## Provider Configuration")
	}
	
	p := []providers.Provider{}

	for _, v := range providers.Providers {
		p = append(p, v)
	}

	slices.SortFunc(p, func(a, b providers.Provider) int {
		return strings.Compare(*a.NamePretty, *b.NamePretty)
	})

	for _, v := range p {
		if provider == "" || provider == strings.ToLower(*v.Name) || provider == strings.ToLower(*v.NamePretty) {
			v.PrintDoc()	
		}
	}
}

func PrintConfig(c any, name string) {
	fmt.Printf("`~/.config/elephant/%s.toml`\n", name)
	printStructTable(c, getStructName(c))
}

func getStructName(c any) string {
	val := reflect.ValueOf(c)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	return val.Type().Name()
}

func printStructTable(c any, structName string) {
	fmt.Printf("#### %s\n", structName)
	fmt.Println("| Field | Type | Default | Description |")
	fmt.Println("| --- | ---- | ---- | --- |")

	printStructDesc(c)
	fmt.Println()
}

func printStructDesc(c any) {
	val := reflect.ValueOf(c)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		fmt.Println("Not a struct")
		return
	}

	typ := val.Type()
	var nestedStructs []reflect.Type

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		if field.PkgPath == "" {
			if field.Anonymous {
				printStructDesc(fieldValue.Interface())
				continue
			}

			name := field.Tag.Get("koanf")

			if name == "" {
				name = field.Tag.Get("toml")
			}

			if name == "-" {
				continue
			}

			fmt.Printf("|%s|%s|%s|%s|\n",
				name, field.Type, field.Tag.Get("default"), field.Tag.Get("desc"))

			if field.Type.Kind() == reflect.Slice {
				elemType := field.Type.Elem()
				if elemType.Kind() == reflect.Struct {
					nestedStructs = append(nestedStructs, elemType)
				}
			}
		}
	}

	for _, structType := range nestedStructs {
		elemVal := reflect.New(structType).Elem()
		printStructTable(elemVal.Interface(), structType.Name())
	}
}
