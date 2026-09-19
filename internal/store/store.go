package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
)

const CurrentSchema = 2

func LoadDB() (model.Database, error) {
	data, err := os.ReadFile(func() string {
		p, _ := config.NewPaths(false)
		return p.Database
	}())
	if os.IsNotExist(err) {
		return model.Database{SchemaVersion: CurrentSchema, ABI: config.DefaultSystemABI, Packages: map[string]model.InstalledPackage{}}, nil
	}
	if err != nil { return model.Database{}, err }
	var db model.Database
	if err := json.Unmarshal(data, &db); err != nil {
		return model.Database{}, fmt.Errorf("invalid database: %w", err)
	}
	if db.SchemaVersion == 0 { db.SchemaVersion = 1 }
	if db.Packages == nil { db.Packages = map[string]model.InstalledPackage{} }
	for name, p := range db.Packages {
		if p.Name == "" { p.Name = name }
		if p.FileHashes == nil { p.FileHashes = map[string]string{} }
		if p.ConfigHashes == nil { p.ConfigHashes = map[string]string{} }
		if p.Manifest == nil && len(p.Files) > 0 {
			for _, f := range p.Files { p.Manifest = append(p.Manifest, model.FileEntry{Path:f, Type:"file"}) }
		}
		db.Packages[name] = p
	}
	if db.ABI == "" { db.ABI = config.DefaultSystemABI }
	return db, nil
}

func pathForUser() (string,error) {
	p,err:=config.NewPaths(true); if err!=nil { return "",err }; return p.Database,nil
}

func LoadDBFor(user bool) (model.Database,error) {
	p,err:=config.NewPaths(user); if err!=nil{return model.Database{},err}
	data,err:=os.ReadFile(p.Database)
	if os.IsNotExist(err){return model.Database{SchemaVersion:CurrentSchema,ABI:config.DefaultSystemABI,Packages:map[string]model.InstalledPackage{}},nil}
	if err!=nil{return model.Database{},err}
	var db model.Database
	if err:=json.Unmarshal(data,&db);err!=nil{return model.Database{},fmt.Errorf("invalid database: %w",err)}
	if db.SchemaVersion==0{db.SchemaVersion=1}
	if db.Packages==nil{db.Packages=map[string]model.InstalledPackage{}}
	if db.ABI==""{db.ABI=config.DefaultSystemABI}
	for name,pkg:=range db.Packages{
		if pkg.Name==""{pkg.Name=name}
		if pkg.FileHashes==nil{pkg.FileHashes=map[string]string{}}
		if pkg.ConfigHashes==nil{pkg.ConfigHashes=map[string]string{}}
		if len(pkg.Manifest)==0 && len(pkg.Files)>0{for _,f:=range pkg.Files{pkg.Manifest=append(pkg.Manifest,model.FileEntry{Path:f,Type:"file"})}}
		db.Packages[name]=pkg
	}
	return db,nil
}

func SaveDBFor(user bool, db model.Database) error {
	p,err:=config.NewPaths(user); if err!=nil{return err}
	if err:=os.MkdirAll(filepath.Dir(p.Database),0o755);err!=nil{return err}
	db.SchemaVersion=CurrentSchema
	data,err:=json.MarshalIndent(db,"","  ");if err!=nil{return err}
	tmp,err:=os.CreateTemp(filepath.Dir(p.Database),"database-*.tmp");if err!=nil{return err}
	tmpPath:=tmp.Name();defer os.Remove(tmpPath)
	if _,err:=tmp.Write(append(data,'\n'));err!=nil{_ = tmp.Close();return err}
	if err:=tmp.Sync();err!=nil{_ = tmp.Close();return err}
	if err:=tmp.Close();err!=nil{return err}
	return os.Rename(tmpPath,p.Database)
}

func SaveDB(db model.Database) error { return SaveDBFor(false,db) }

func AddTransactionFor(user bool, tx model.Transaction) error {
	db,err:=LoadDBFor(user);if err!=nil{return err}
	db.Transactions=append(db.Transactions,tx)
	if len(db.Transactions)>200{db.Transactions=db.Transactions[len(db.Transactions)-200:]}
	return SaveDBFor(user,db)
}
func UpdateTransactionFor(user bool, tx model.Transaction) error {
	db,err:=LoadDBFor(user);if err!=nil{return err}
	for i:=range db.Transactions{if db.Transactions[i].ID==tx.ID{db.Transactions[i]=tx;return SaveDBFor(user,db)}}
	return fmt.Errorf("transaction %s not found",tx.ID)
}
func GetTransactionFor(user bool,id string)(model.Transaction,error){
	db,err:=LoadDBFor(user);if err!=nil{return model.Transaction{},err}
	for _,tx:=range db.Transactions{if tx.ID==id{return tx,nil}}
	return model.Transaction{},fmt.Errorf("transaction %s not found",id)
}

func AddTransaction(tx model.Transaction) error { return AddTransactionFor(false,tx) }
func UpdateTransaction(tx model.Transaction) error { return UpdateTransactionFor(false,tx) }
func GetTransaction(id string)(model.Transaction,error){return GetTransactionFor(false,id)}

func SaveSnapshotFor(user bool, s model.Snapshot) error {
	db,err:=LoadDBFor(user);if err!=nil{return err}
	db.Snapshots=append(db.Snapshots,s)
	sort.Slice(db.Snapshots,func(i,j int)bool{return db.Snapshots[i].CreatedAt.Before(db.Snapshots[j].CreatedAt)})
	return SaveDBFor(user,db)
}
