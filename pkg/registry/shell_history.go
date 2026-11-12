package registry

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
 "time"
)

// ShellSessionEntry is durable metadata, not an SSH connection. An active
// connection exists only in the Engine process that opened the shell.
type ShellSessionEntry struct {
 ID string
 VMID string
 Status string
 CreatedAt string
 UpdatedAt string
 ClosedAt string
}

// ShellCommandEntry contains the exact submitted command and bounded merged
// guest output. This may contain sensitive content; protect the SQLite file.
type ShellCommandEntry struct {
 ID string
 ShellID string
 VMID string
 Command string
 Output []byte
 Status string
 ExitCode *int
 Error string
 StartedAt string
 FinishedAt string
}

func (s *Store) InsertShellSession(ctx context.Context, id, vmID string) error {
 if ctx == nil { return errors.New("context is required") }
 if id == "" || vmID == "" { return errors.New("shell and VM IDs are required") }
 now := time.Now().UTC().Format(time.RFC3339Nano)
 _, err := s.db.ExecContext(ctx,`INSERT INTO shell_sessions(id,vm_id,status,created_at,updated_at) VALUES(?,?,'active',?,?)`,id,vmID,now,now)
 return err
}

func (s *Store) SetShellSessionStatus(ctx context.Context, id, status string) error {
 if ctx == nil { return errors.New("context is required") }
 if status != "closed" && status != "disconnected" { return errors.New("invalid shell status") }
 now := time.Now().UTC().Format(time.RFC3339Nano)
 r,err := s.db.ExecContext(ctx,`UPDATE shell_sessions SET status=?,updated_at=?,closed_at=?
 WHERE id=? AND status='active'`,status,now,now,id)
 if err!=nil{return err}
 n,err:=r.RowsAffected()
 if err!=nil{return err}
 if n!=1{return fmt.Errorf("active shell %s not found",id)}
 return nil
}

func (s *Store) GetShellSession(ctx context.Context,id string)(ShellSessionEntry,error){
 if ctx==nil{return ShellSessionEntry{},errors.New("context is required")}
 var v ShellSessionEntry
 err:=s.db.QueryRowContext(ctx,`SELECT id,vm_id,status,created_at,updated_at,closed_at FROM shell_sessions WHERE id=?`,id).
 Scan(&v.ID,&v.VMID,&v.Status,&v.CreatedAt,&v.UpdatedAt,&v.ClosedAt)
 if errors.Is(err,sql.ErrNoRows){return v,fmt.Errorf("shell %s not found: %w",id,err)}
 return v,err
}

// ListShellSessions returns a VM's durable session IDs even after guest cleanup.
func (s *Store) ListShellSessions(ctx context.Context,vmID string)([]ShellSessionEntry,error){
 if ctx==nil{return nil,errors.New("context is required")}
 if vmID==""{return nil,errors.New("VM ID is required")}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,vm_id,status,created_at,updated_at,closed_at
 FROM shell_sessions WHERE vm_id=? ORDER BY created_at DESC,id DESC`,vmID)
 if err!=nil{return nil,err}
 defer rows.Close()
 result:=[]ShellSessionEntry{}
 for rows.Next(){
  var v ShellSessionEntry
  if err:=rows.Scan(&v.ID,&v.VMID,&v.Status,&v.CreatedAt,&v.UpdatedAt,&v.ClosedAt);err!=nil{return nil,err}
  result=append(result,v)
 }
 return result,rows.Err()
}

func (s *Store) BeginShellCommand(ctx context.Context, entry ShellCommandEntry)error{
 if ctx==nil{return errors.New("context is required")}
 if entry.ID==""||entry.ShellID==""||entry.VMID==""||entry.Command==""{
  return errors.New("command ID, shell ID, VM ID and command are required")
 }
 when:=time.Now().UTC().Format(time.RFC3339Nano)
 _,err:=s.db.ExecContext(ctx,`INSERT INTO shell_commands(id,shell_id,vm_id,command,status,started_at)
 VALUES(?,?,?,?,'running',?)`,entry.ID,entry.ShellID,entry.VMID,entry.Command,when)
 return err
}

func (s *Store) FinishShellCommand(ctx context.Context,id string, output []byte, exitCode *int, commandErr error)error{
 if ctx==nil{return errors.New("context is required")}
 if id==""{return errors.New("command ID is required")}
 if output==nil{output=[]byte{}}
 status,errText:="completed",""
 if commandErr!=nil{status,errText="failed",commandErr.Error()}
 when:=time.Now().UTC().Format(time.RFC3339Nano)
 r,err:=s.db.ExecContext(ctx,`UPDATE shell_commands SET output=?,status=?,exit_code=?,error=?,finished_at=?
 WHERE id=? AND status='running'`,output,status,exitCode,errText,when,id)
 if err!=nil{return err}
 n,err:=r.RowsAffected()
 if err!=nil{return err}
 if n!=1{return fmt.Errorf("running command %s not found",id)}
 return nil
}

func (s *Store) GetShellCommand(ctx context.Context,id string)(ShellCommandEntry,error){
 if ctx==nil{return ShellCommandEntry{},errors.New("context is required")}
 var v ShellCommandEntry
 var code sql.NullInt64
 err:=s.db.QueryRowContext(ctx,`SELECT id,shell_id,vm_id,command,output,status,exit_code,error,started_at,finished_at
 FROM shell_commands WHERE id=?`,id).Scan(
 &v.ID,&v.ShellID,&v.VMID,&v.Command,&v.Output,&v.Status,&code,&v.Error,&v.StartedAt,&v.FinishedAt)
 if code.Valid{n:=int(code.Int64);v.ExitCode=&n}
 if errors.Is(err,sql.ErrNoRows){return v,fmt.Errorf("command %s not found: %w",id,err)}
 return v,err
}

func (s *Store) ListShellCommands(ctx context.Context,shellID string)([]ShellCommandEntry,error){
 if ctx==nil{return nil,errors.New("context is required")}
 if shellID==""{return nil,errors.New("shell ID is required")}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,shell_id,vm_id,command,output,status,exit_code,error,started_at,finished_at
 FROM shell_commands WHERE shell_id=? ORDER BY started_at,id`,shellID)
 if err!=nil{return nil,err}
 defer rows.Close()
 results:=[]ShellCommandEntry{}
 for rows.Next(){
  var v ShellCommandEntry
  var code sql.NullInt64
  if err:=rows.Scan(&v.ID,&v.ShellID,&v.VMID,&v.Command,&v.Output,&v.Status,&code,&v.Error,&v.StartedAt,&v.FinishedAt);err!=nil{return nil,err}
  if code.Valid{n:=int(code.Int64);v.ExitCode=&n}
  results=append(results,v)
 }
 return results,rows.Err()
}
