package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yang1997434/rex-voice/internal/config"
	"github.com/yang1997434/rex-voice/internal/pair"
)

func pairPath(dir string) string { return filepath.Join(dir, "pair.json") }

func pairCmd(args []string) int {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	dirFlag := configDirFlag(fs)
	relay := fs.String("relay", "", "relay WebSocket URL put into the QR code and saved to config.json (default: config.json relay)")
	force := fs.Bool("force", false, "replace an existing pairing (the paired phone stops working)")
	fs.Parse(args)

	dir, c, err := loadConfig(*dirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *relay != "" && *relay != c.Relay {
		// serve must dial the relay the phone was told about
		if err := saveRelay(dir, *relay); err != nil {
			fmt.Fprintln(os.Stderr, "pair:", err)
			return 1
		}
	}
	if *relay == "" {
		*relay = c.Relay
	}
	path := pairPath(dir)
	old, err := pair.Load(path)
	switch {
	case err == nil && !old.PairedAt.IsZero() && !*force:
		fmt.Fprintf(os.Stderr, "已经和一台手机配对（%s）。重新配对会让已配对手机失效；确定要换就加 -force。\n", old.PairedAt.Format("2006-01-02 15:04"))
		return 1
	case err != nil && !errors.Is(err, os.ErrNotExist) && !*force:
		fmt.Fprintf(os.Stderr, "读不了现有配对文件：%v\n要覆盖就加 -force。\n", err)
		return 1
	}
	p, err := pair.Generate()
	if err == nil {
		err = p.Save(path)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pair:", err)
		return 1
	}
	u := p.URL(*relay)
	fmt.Print(pair.QRText(u))
	fmt.Println(u)
	fmt.Println("已保存到", path, "；用 RexVoice app 扫上面的二维码。")
	fmt.Println("下一步：rex-voiced install（装成后台服务）")
	return 0
}

func saveRelay(dir, relay string) error {
	c, err := config.Read(dir)
	if err != nil {
		return err
	}
	c.Relay = relay
	return c.Save(dir)
}
