package integration

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "gopkg.in/yaml.v3"
)

// The expensive ZIP/SSH VM E2E runs only when a local develop branch is
// promoted to homolog or main. Routine feature PRs must never launch KVM.
func TestIntegral2PromotionOnlyTriggerContract(t *testing.T){
 path:=filepath.Join("..","..",".github","workflows","integral-2-zip.yml")
 raw,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 var cfg struct {
  On map[string]struct{
   Branches []string \`yaml:"branches"\`
   Types []string \`yaml:"types"\`
  } \`yaml:"on"\`
  Jobs map[string]struct{If string \`yaml:"if"\`} \`yaml:"jobs"\`
  Permissions map[string]string \`yaml:"permissions"\`
 }
 if err:=yaml.Unmarshal(raw,&cfg);err!=nil{t.Fatal(err)}
 if len(cfg.On)!=1 {t.Fatalf("expensive ZIP test must be PR-only: %+v",cfg.On)}
 pr,ok:=cfg.On["pull_request"]
 if !ok||len(pr.Branches)!=2||pr.Branches[0]!="homolog"||pr.Branches[1]!="main"{
  t.Fatalf("only homolog/main PR destinations allowed: %+v",cfg.On)
 }
 if len(pr.Types)!=3||pr.Types[0]!="opened"||pr.Types[1]!="synchronize"||pr.Types[2]!="reopened"{
  t.Fatalf("only lifecycle/review commits may trigger ZIP E2E: %+v",pr.Types)
 }
 if len(cfg.Jobs)!=1{t.Fatalf("one expensive ZIP job expected: %+v",cfg.Jobs)}
 job,ok:=cfg.Jobs["integral-2-zip-upload-command"]
 if !ok||!strings.Contains(job.If,"github.head_ref == 'develop'")||
  !strings.Contains(job.If,"github.event.pull_request.head.repo.full_name == github.repository"){
  t.Fatalf("expensive ZIP test must require same-repo develop promotion: %+v",cfg.Jobs)
 }
 if len(cfg.Permissions)!=1||cfg.Permissions["contents"]!="read"{
  t.Fatalf("CI token must be read-only: %+v",cfg.Permissions)
 }
}
