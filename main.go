package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"git.caizhanfuwu.com/sgt/model"
	"github.com/urfave/cli"
)

const (
	initYaml = "init.yaml"
)

func main() {
	app := cli.NewApp()
	app.Name = "spt: sino代码生成工具"
	app.Version = "0.1"

	newCom := cli.Command{
		Name:  "model",
		Usage: "从yaml配置文件生成models代码",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "yaml, y",
				Value: initYaml,
				Usage: "yaml配置文件路径，扩展名.yaml可省略",
			},
		},
		Action: func(c *cli.Context) error {
			yaml := c.String("yaml")
			if len(yaml) == 0 {
				yaml = initYaml
			}
			filename := strings.TrimSuffix(yaml, ".yaml") + ".yaml"
			p, err := model.ParseProjecp(filename)
			if err != nil {
				fmt.Printf("%v\n", err)
				return err
			}
			fmt.Printf("Using yaml config file...: %s\n", filename)
			err = p.Gen()
			if err != nil {
				fmt.Printf("%v\n", err)
				return err
			}
			return nil
		},
	}

	app.Commands = []cli.Command{newCom}

	sort.Sort(cli.CommandsByName(app.Commands))
	sort.Sort(cli.FlagsByName(newCom.Flags))

	app.Run(os.Args)
}
