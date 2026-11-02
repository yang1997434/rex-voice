package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yang1997434/rex-voice/internal/pair"
)

const defaultRelay = "wss://r.maruiao.top/_voice/ws"

func pairPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err) // launchd and login shells both set HOME
	}
	return filepath.Join(home, "Library", "Application Support", "rex-voice", "pair.json")
}

func pairCmd(args []string) int {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	relay := fs.String("relay", defaultRelay, "relay WebSocket URL put into the QR code")
	force := fs.Bool("force", false, "replace an existing pairing (the paired phone stops working)")
	fs.Parse(args)

	path := pairPath()
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
	return 0
}
