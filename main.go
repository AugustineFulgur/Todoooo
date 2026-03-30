package main

import "log"

func main() {
	log.SetFlags(0)

	store, err := NewStore()
	if err != nil {
		log.Fatalf("初始化数据存储失败: %v", err)
	}

	app, err := NewNativeApp(store)
	if err != nil {
		log.Fatalf("初始化原生窗口失败: %v", err)
	}

	stopHotkey, err := registerSummonHotkey(app.SummonFromHotkey)
	if err != nil {
		log.Printf("注册 Ctrl+数字键盘9 失败: %v", err)
	}
	if stopHotkey != nil {
		defer stopHotkey()
	}

	if err := app.Run(); err != nil {
		log.Fatalf("运行应用失败: %v", err)
	}
}
