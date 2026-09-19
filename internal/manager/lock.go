package manager

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

func withLock(state string, fn func() error) error {
	if err:=os.MkdirAll(state,0o755);err!=nil{return err}
	path:=filepath.Join(state,"lock")
	for i:=0;i<20;i++{
		f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0o644)
		if err==nil{
			_,_=f.WriteString(time.Now().UTC().Format(time.RFC3339Nano))
			_ = f.Close()
			defer os.Remove(path)
			return fn()
		}
		if !os.IsExist(err){return err}
		time.Sleep(50*time.Millisecond)
	}
	return errors.New("another yspm transaction is already running")
}
