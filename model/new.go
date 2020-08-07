package model

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	_ "github.com/go-sql-driver/mysql"
	yaml "gopkg.in/yaml.v2"
)

type (
	Project struct {
		Name      string      `yaml:"-"`
		Models    []*Model    `yaml:"models"`
		DbConfigs []*DbConfig `yaml:"db_config"`
		codes     Codes       `yaml:"-"`
	}

	Model struct {
		Name             string   `yaml:"name"` // CamelName
		LowerName        string   `yaml:"-"`
		SnakeName        string   `yaml:"-"`
		LowerFirstName   string   `yaml:"-"`
		LowerFirstLetter string   `yaml:"-"`
		Comment          string   `yaml:"comment,omitempty"`
		Fields           []*Field `yaml:"fields"`
		TableName        string   `yaml:"table_name,omitempty"`
		NameSql          string   `yaml:"-"`
		QuerySql         string   `yaml:"-"`
		UpdateSql        string   `yaml:"-"`
	}

	DbConfig struct {
		Name      string `yaml:"name"` // CamelName
		Database  string `yaml:"database"`
		Username  string `yaml:"username"`
		Password  string `yaml:"password"`
		Host      string `yaml:"host"`
		Port      int32  `yaml:"port"`
		Table     string `yaml:"table"`
		AliasName string `yaml:"alias_name"`
	}

	Field struct {
		Name         string `yaml:"name"`
		SnakeName    string `yaml:"-"`
		OriginName   string `yaml:"-"`
		Type         string `yaml:"type"`
		OfficialType string `yaml:"-"`
		Tag          string `yaml:"tag,omitempty"`
		OfficialTag  string `yaml:"-"`
		Comment      string `yaml:"comment,omitempty"`
	}

	Codes struct {
		Models []File
	}

	File struct {
		Name   string
		Buffer *bytes.Buffer
	}
)

func tableMetaToModel(tableMeta *TableMeta) *Model {
	model := &Model{
		Name:             CamelString(tableMeta.Name),
		LowerName:        strings.ToLower(CamelString(tableMeta.Name)),
		TableName:        tableMeta.AliasName,
		Fields:           []*Field{},
		SnakeName:        SnakeString(tableMeta.Name),
		LowerFirstLetter: strings.ToLower(tableMeta.Name[:1]),
		LowerFirstName:   strings.ToLower(tableMeta.Name[:1]) + CamelString(tableMeta.Name)[1:],
	}
	for _, field := range tableMeta.Fields {
		if field.Field == "id" || field.Field == "created" || field.Field == "updated" || field.Field == "deleted" {
			continue
		}
		model.Fields = append(model.Fields, &Field{
			Name:       field.Field,
			Type:       field.GoType,
			OriginName: field.Field,
		})
	}
	return model
}

func (p *Project) Gen() error {
	tableInfos := p.GetTablesInfos()
	for _, tableInfo := range tableInfos {
		m := tableMetaToModel(tableInfo)
		p.Models = append(p.Models, m)
	}
	for _, model := range p.Models {
		err := p.checkFields(model.Fields, "model", model.Name, true)
		if err != nil {
			log.Fatal(err)
		}
	}
	err := p.makeModelsCodesFromStruct()
	if err != nil {
		return err
	}
	err = p.writeFiles()
	if err != nil {
		return err
	}
	fmt.Printf("Created success!\n")
	return nil
}

func genSource(username, pwd, host, dbname string, port int32) (source string) {
	var (
		defaultPort int32  = 3306
		defaultPwd  string = ""
	)
	if port > 0 {
		defaultPort = port
	}
	if pwd != "" {
		defaultPwd = ":" + pwd
	}

	return fmt.Sprintf("%s%s@tcp(%s:%d)/%s?charset=utf8mb4,utf8", username, defaultPwd, host, defaultPort, dbname)
}

func isInArray(arr []string, val string) bool {
	for _, v := range arr {
		if v == val {
			return true
		}
	}
	return false
}

type ColumnInfo struct {
	Field    string
	Type     string
	Null     string
	Key      string
	Default  *string
	Extra    string
	GoType   string
	Required string
}

var typeMap = [][]string{
	{"int", "int32"},
	{"tinyint", "int8"},
	{"bigint", "int64"},
	{"varchar", "string"},
	{"char", "string"},
	{"text", "string"},
	{"tinytext", "string"},
	{"datetime", "time.Time"},
	{"date", "time.Time"},
	{"timestamp", "time.Time"},
	{"time", "string"},
	{"decimal", "float64"},
	{"bool", "bool"},
	{"json", "string"},
}

func toGoType(s string) string {
	for _, v := range typeMap {
		if strings.HasPrefix(s, v[0]) {
			return v[1]
		}
	}
	log.Fatalf("unsupport type %s", s)
	return ""
}

type TableMeta struct {
	Name      string
	AliasName string
	Fields    []*ColumnInfo
}

func loadTableMeta(db *sql.DB, name, aliasName string) *TableMeta {
	rows, err := db.Query("SHOW COLUMNS FROM `" + name + "`")
	if err != nil {
		log.Fatal(err)
	}
	cis := []*ColumnInfo{}

	for rows.Next() {
		ci := &ColumnInfo{}
		err = rows.Scan(&(ci.Field), &(ci.Type), &(ci.Null), &(ci.Key), &(ci.Default), &(ci.Extra))
		if err != nil {
			log.Fatal(err)
		}
		ci.GoType = toGoType(ci.Type)
		ci.Required = ",required"
		if ci.Null == "YES" {
			ci.Required = ""
		}
		cis = append(cis, ci)
	}
	return &TableMeta{
		Name:      aliasName,
		AliasName: aliasName,
		Fields:    cis,
	}
}

func loadDbMeta(source, table, aliasName string) []*TableMeta {
	db, err := sql.Open("mysql", source)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SHOW TABLES")
	if err != nil {
		log.Fatal(err)
	}
	tableAddr := strings.Split(table, ",")
	tableInfos := []*TableMeta{}
	for rows.Next() {
		var name string
		err = rows.Scan(&name)
		if err != nil {
			log.Fatal(err)
		}
		if table == "*" || isInArray(tableAddr, name) {
			tableInfo := loadTableMeta(db, table, aliasName)
			tableInfos = append(tableInfos, tableInfo)
		}
	}
	return tableInfos
}

func (p *Project) GetTablesInfos() []*TableMeta {
	result := []*TableMeta{}
	for _, dbConfig := range p.DbConfigs {
		source := genSource(dbConfig.Username, dbConfig.Password, dbConfig.Host, dbConfig.Database, dbConfig.Port)
		tableInfos := loadDbMeta(source, dbConfig.Table, dbConfig.AliasName)
		result = append(result, tableInfos...)
	}
	return result
}

func (p *Project) makeModelsCodesFromStruct() error {
	for _, model := range p.Models {
		modelFile := File{
			Name:   "models/" + model.SnakeName + "/gen_" + model.SnakeName + ".go",
			Buffer: bytes.NewBuffer(nil),
		}
		model.NameSql = fmt.Sprintf("`%s`", model.SnakeName)
		for _, field := range model.Fields {
			if field.SnakeName[0] == '_' {
				continue
			}
			model.QuerySql += fmt.Sprintf("`%s`, ", field.OriginName)
		}
		m, err := template.New(modelFile.Name).Parse(
			`// Code generated by sgt.
// DO NOT EDIT!
package {{.LowerName}}

import (
	"fmt"
	"time"

	"git.caizhanfuwu.com/sinolive/models"
	"git.caizhanfuwu.com/sinolive/utils"
	"github.com/astaxie/beego/orm"
)

// {{.Name}} {{if .Comment}}{{.Comment}}{{else}}struct{{end}}
type {{.Name}} struct {
	Id        int64  ` + "`orm:\"pk;column(id);\"`" + ` // ID{{range .Fields}}
	{{.Name}} {{.OfficialType}} {{.OfficialTag}}{{if .Comment}} // {{.Comment}}{{end}}{{end}}
	Created   int64  ` + "`orm:\"column(created)\"`" + ` // 创建时间
	Updated   int64  ` + "`orm:\"column(updated)\"`" + ` // 更新时间
	Deleted   int8   ` + "`orm:\"column(deleted)\"`" + ` // 0未删除 1已删除
}

func (this *{{.Name}}) TableName() string {
	return models.TableName("{{.TableName}}")
}

// Add{{.Name}} insert a {{.Name}} data into database.
func Add{{.Name}}({{.LowerFirstLetter}} *{{.Name}}) (int64, error) {
	o := orm.NewOrm()

	ts := time.Now().Unix()
	{{.LowerFirstLetter}}.Created = ts
	{{.LowerFirstLetter}}.Updated = ts
	return o.Insert({{.LowerFirstLetter}})
}

// Get{{.Name}} query a {{.Name}} data from DB by id.
func Get{{.Name}}(id int64) (*{{.Name}}, error) {
	{{.LowerFirstLetter}}:= &{{.Name}}{}
	err := utils.GetCache("Get{{.Name}}.id."+fmt.Sprintf("%d", id), {{.LowerFirstLetter}})
	if err != nil {
		o := orm.NewOrm()
		{{.LowerFirstLetter}}.Id = id
		err = o.Read({{.LowerFirstLetter}})
		if err != nil {
			return nil, err
		}
		utils.SetCache("Get{{.Name}}.id."+fmt.Sprintf("%d", id), {{.LowerFirstLetter}}, 60)
		return {{.LowerFirstLetter}}, nil
	}
	return {{.LowerFirstLetter}}, nil
}

// Get{{.Name}}ByWhere query a {{.Name}} data from DB by WHERE condition.
func Get{{.Name}}ByWhere(whereCond string, args ...interface{}) (*{{.Name}}, error) {
	o := orm.NewOrm()
	{{.LowerFirstLetter}} := &{{.Name}}{}
	err := o.Raw("SELECT id, {{.QuerySql}}created, updated, deleted FROM "+{{.LowerFirstLetter}}.TableName()+" WHERE "+whereCond+" LIMIT 1", args...).QueryRow({{.LowerFirstLetter}})
	if err != nil {
		return nil, err
	}
	return {{.LowerFirstLetter}}, nil
}

// Select{{.Name}}ByWhere query some {{.Name}} data from DB by WHERE condition.
func Select{{.Name}}ByWhere(whereCond string, args ...interface{}) (num int64, {{.LowerFirstLetter}}s []*{{.Name}}, err error) {
	o := orm.NewOrm()
	num, err = o.Raw("SELECT id, {{.QuerySql}}created, updated, deleted FROM "+new({{.Name}}).TableName()+" WHERE "+whereCond, args...).QueryRows(&{{.LowerFirstLetter}}s)
	return
}

// Count{{.Name}}ByWhere count {{.Name}} data number from DB by WHERE condition.
func Count{{.Name}}ByWhere(whereCond string, args ...interface{}) (count int64, err error) {
	o := orm.NewOrm()
	err = o.Raw("SELECT count(1) FROM "+new({{.Name}}).TableName()+" WHERE "+whereCond, args...).QueryRow(&count)
	return
}

// Update{{.Name}} update the {{.Name}} data in DB by id.
func Update{{.Name}}(updPro *{{.Name}}) error {
	o := orm.NewOrm()

	updPro.Updated = time.Now().Unix()
	_, err := o.Update(updPro, {{.QuerySql}}` + "`updated`, `deleted`" + `)
	utils.DelCache(fmt.Sprintf("Get{{.Name}}.id.%d", updPro.Id))
	return err
}

func init() {
	orm.RegisterModel(new({{.Name}}))
}`)
		if err != nil {
			return err
		}
		err = m.Execute(modelFile.Buffer, model)
		if err != nil {
			return err
		}
		p.codes.Models = append(p.codes.Models, modelFile)
	}
	return nil
}

func (p *Project) writeFiles() error {
	for _, m := range p.codes.Models {
		os.MkdirAll(filepath.Dir(m.Name), 0775)
		err := ioutil.WriteFile(m.Name, m.Buffer.Bytes(), 0664)
		if err != nil {
			return err
		}
	}

	cmd := exec.Command("gofmt", "-w", ".")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func ParseProjecp(filename string) (*Project, error) {
	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Printf("ioutil.ReadFile(%s) err: %v\n", filename, err)
		return nil, err
	}
	p := &Project{}
	err = yaml.Unmarshal(yamlFile, p)
	if err != nil {
		return nil, err
	}
	p.codes = Codes{
		Models: []File{},
	}

	var modelkeys = make(map[string]bool, len(p.Models))
	for _, model := range p.Models {
		log.Printf("model.Name: %s\n", model.Name)
		if len(model.Name) == 0 {
			return nil, errors.New("model name can not be empty")
		}
		modelname := model.Name
		model.Name = CamelString(model.Name)
		model.LowerName = strings.ToLower(model.Name)
		model.SnakeName = SnakeString(model.Name)
		model.LowerFirstLetter = strings.ToLower(model.Name[:1])
		model.LowerFirstName = model.LowerFirstLetter + model.Name[1:]
		if model.TableName == "" {
			model.TableName = model.SnakeName
		}

		if modelkeys[model.Name] {
			return nil, fmt.Errorf("duplicate defined: model: %s", modelname)
		} else {
			modelkeys[model.Name] = true
		}

	}
	for _, dbConfig := range p.DbConfigs {
		if dbConfig.AliasName == "" {
			dbConfig.AliasName = dbConfig.Name
		}
	}
	return p, err
}

func (p *Project) checkFields(fields []*Field, parentKey string, parentVal string, isInModel bool) error {
	pkeys := make(map[string]bool, len(fields))
	for _, field := range fields {
		name := field.Name
		field.Name = CamelString(field.Name)
		if pkeys[field.Name] {
			return fmt.Errorf("duplicate defined: %s: %s, field: %s", parentKey, parentVal, name)
		} else {
			pkeys[field.Name] = true
		}

		field.OfficialType = field.Type
		field.SnakeName = SnakeString(field.Name)
		field.Tag = strings.Trim(field.Tag, "`")
		field.Tag = strings.Trim(field.Tag, " ")
		if field.Name[0] == '_' {
			if len(field.Tag) > 0 {
				field.OfficialTag = "`" + field.Tag + "`"
			}
			continue
		}
		if len(field.Tag) == 0 {
			if isInModel {
				field.OfficialTag = "`json:\"" + field.OriginName + `" orm:"column(` + field.OriginName + `)` + "\"`"
			} else {
				field.OfficialTag = "`json:\"" + field.OriginName + "\"`"
			}
		} else if !strings.Contains(strings.Replace(field.Tag, " ", "", -1), `json:"`) {
			field.OfficialTag = "`json:\"" + field.OriginName + `" ` + field.Tag + "`"
		} else {
			field.OfficialTag = "`" + field.Tag + "`"
		}
	}

	return nil
}
