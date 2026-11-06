// Package registry persists the lifecycle of disposable VMs in a private
// SQLite database shared by the InsulaVM CLI and any Go library consumers.
package registry

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "time"

 _ "modernc.org/sqlite"
)

// Entry is VM metadata; PRIVATE KEY CONTENTS are never stored in SQLite.
type Entry struct {
 ID string
 LibvirtUUID string
 Name string
 Profile string
 IP string
 MAC string
 HostFingerprint string
 SudoMode string
 SSHUser string
 PrivateKeyPath string
 KnownHostsPath string
 InstanceDir string
 Status string
 CreatedAt string
 UpdatedAt string
 FinishedAt string
}

type Store struct{db *sql.DB}

// Open opens or creates a SQLite database with a restrictive filesystem
// mode. WAL and busy_timeout support multiple independent host processes.
func Open(filename string)(*Store,error){
 if filename==""{return nil,errors.New("SQLite filename is required")}
 if err:=os.MkdirAll(filepath.Dir(filename),0700);err!=nil{return nil,err}
 f,err:=os.OpenFile(filename,os.O_CREATE|os.O_RDWR,0600)
 if err!=nil{return nil,err}
 if err:=f.Close();err!=nil{return nil,err}
 db,err:=sql.Open("sqlite",filename)
 if err!=nil{return nil,err}
 db.SetMaxOpenConns(1)
 s:=&Store{db:db}
 for _,query:=range []string{
  "PRAGMA busy_timeout=10000",
  "PRAGMA journal_mode=WAL",
  `CREATE TABLE IF NOT EXISTS vm_instances (
   id TEXT PRIMARY KEY,
   libvirt_uuid TEXT NOT NULL UNIQUE,
   name TEXT NOT NULL UNIQUE,
   profile TEXT NOT NULL,
   ip TEXT NOT NULL DEFAULT '',
   mac TEXT NOT NULL,
   host_fingerprint TEXT NOT NULL,
   sudo_mode TEXT NOT NULL,
   ssh_user TEXT NOT NULL,
   private_key_path TEXT NOT NULL,
   known_hosts_path TEXT NOT NULL,
   instance_dir TEXT NOT NULL,
   status TEXT NOT NULL CHECK(status IN ('creating','running','failed','destroyed')),
   created_at TEXT NOT NULL,
   updated_at TEXT NOT NULL,
   finished_at TEXT NOT NULL DEFAULT ''
  )`,
  "CREATE INDEX IF NOT EXISTS vm_instances_by_status ON vm_instances(status,created_at)",
 }{
  if _,err:=db.Exec(query);err!=nil{_ = db.Close();return nil,fmt.Errorf("initialize SQLite registry: %w",err)}
 }
 return s,nil
}
func(s *Store)Close()error{return s.db.Close()}

func(s *Store)Insert(ctx context.Context,e Entry)error{
 if ctx==nil{return errors.New("context is required")}
 now:=time.Now().UTC().Format(time.RFC3339Nano)
 _,err:=s.db.ExecContext(ctx,`INSERT INTO vm_instances(
  id,libvirt_uuid,name,profile,ip,mac,host_fingerprint,sudo_mode,ssh_user,private_key_path,known_hosts_path,
  instance_dir,status,created_at,updated_at
 ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
  e.ID,e.LibvirtUUID,e.Name,e.Profile,e.IP,e.MAC,e.HostFingerprint,e.SudoMode,e.SSHUser,e.PrivateKeyPath,e.KnownHostsPath,
  e.InstanceDir,"creating",now,now)
 return err
}

func(s *Store)SetRunning(ctx context.Context,id,ip string)error{
 return s.update(ctx,id,"running",ip,false)
}
func(s *Store)SetFailed(ctx context.Context,id string)error{
 return s.update(ctx,id,"failed","",true)
}
func(s *Store)SetDestroyed(ctx context.Context,id string)error{
 return s.update(ctx,id,"destroyed","",true)
}
func(s *Store)update(ctx context.Context,id,status,ip string,finished bool)error{
 if ctx==nil{return errors.New("context is required")}
 now:=time.Now().UTC().Format(time.RFC3339Nano)
 query:=`UPDATE vm_instances SET status=?,updated_at=?,ip=CASE WHEN ?<>'' THEN ? ELSE ip END,
  finished_at=CASE WHEN ? THEN ? ELSE finished_at END
 WHERE id=?`
 result,err:=s.db.ExecContext(ctx,query,status,now,ip,ip,finished,now,id)
 if err!=nil{return err}
 n,err:=result.RowsAffected()
 if err!=nil{return err}
 if n!=1{return fmt.Errorf("VM %s not found",id)}
 return nil
}
const fields="id,libvirt_uuid,name,profile,ip,mac,host_fingerprint,sudo_mode,ssh_user,private_key_path,known_hosts_path,instance_dir,status,created_at,updated_at,finished_at"

func scan(row interface{Scan(...any)error})(Entry,error){
 var e Entry
 err:=row.Scan(&e.ID,&e.LibvirtUUID,&e.Name,&e.Profile,&e.IP,&e.MAC,
  &e.HostFingerprint,&e.SudoMode,&e.SSHUser,&e.PrivateKeyPath,&e.KnownHostsPath,&e.InstanceDir,&e.Status,
  &e.CreatedAt,&e.UpdatedAt,&e.FinishedAt)
 return e,err
}

func(s *Store)Get(ctx context.Context,id string)(Entry,error){
 if ctx==nil{return Entry{},errors.New("context is required")}
 e,err:=scan(s.db.QueryRowContext(ctx,"SELECT "+fields+" FROM vm_instances WHERE id=?",id))
 if errors.Is(err,sql.ErrNoRows){return Entry{},fmt.Errorf("VM %s not found: %w",id,err)}
 return e,err
}

func(s *Store)List(ctx context.Context)([]Entry,error){
 if ctx==nil{return nil,errors.New("context is required")}
 rows,err:=s.db.QueryContext(ctx,"SELECT "+fields+" FROM vm_instances ORDER BY created_at DESC")
 if err!=nil{return nil,err}
 defer rows.Close()
 result:=[]Entry{}
 for rows.Next(){
  e,err:=scan(rows)
  if err!=nil{return nil,err}
  result=append(result,e)
 }
 return result,rows.Err()
}
