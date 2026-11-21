package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
	"github.com/WishingSkin/WishingSkin/internal/bootstrap"
	"github.com/WishingSkin/WishingSkin/internal/store"
)

func createAdmin(ctx context.Context, s *store.Store) error {
	if len(os.Args) != 2 {
		return errors.New("create-admin 不接受参数，请在本机终端交互输入；密码不要放在命令或环境变量中")
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) || !term.IsTerminal(int(os.Stderr.Fd())) {
		return errors.New("create-admin 需要交互终端，不接受重定向或管道密码")
	}
	previous, err := term.MakeRaw(fd)
	if err != nil {
		return errors.New("无法启用隐藏密码输入")
	}
	defer term.Restore(fd, previous)
	console := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stderr}, "邮箱: ")
	_, _ = fmt.Fprintln(console, "创建首个管理员。仅限空账号库；不会开启注册。")
	email, err := console.ReadLine()
	if err != nil {
		return errors.New("已取消管理员初始化")
	}
	console.SetPrompt("昵称: ")
	nickname, err := console.ReadLine()
	if err != nil {
		return errors.New("已取消管理员初始化")
	}
	_, _ = fmt.Fprintln(console, "密码须为 8–128 个字符，包含英文字母、数字和符号。")
	password, err := console.ReadPassword("新密码（隐藏输入）: ")
	if err != nil {
		return errors.New("已取消管理员初始化")
	}
	confirmation, err := console.ReadPassword("确认新密码（隐藏输入）: ")
	if err != nil {
		return errors.New("已取消管理员初始化")
	}
	if password != confirmation {
		return errors.New("两次输入的新密码不一致，未创建账号")
	}
	_, err = bootstrap.CreateAdmin(ctx, s, email, nickname, password)
	password, confirmation = "", ""
	if errors.Is(err, bootstrap.ErrNotEmpty) {
		return err
	}
	if err != nil {
		return errors.New("管理员初始化未完成，请检查输入与数据库；不会覆盖已有账号")
	}
	_, _ = fmt.Fprintln(console, "管理员已创建，注册仍关闭。可登录配置站点；邮箱尚未验证。")
	return nil
}
