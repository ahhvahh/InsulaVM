package manifest

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestManifestReadFileRoundtripAndErrors(t *testing.T){
    path:=filepath.Join(t.TempDir(),"insula.yaml")
    if err:=os.WriteFile(path,[]byte(good),0600);err!=nil{t.Fatal(err)}
    result,err:=ReadFile(path)
    if err!=nil||result.Repository.Name!="demo"||result.GroupName()!="ci"{
        t.Fatalf("valid manifest failed: %+v %v",result,err)
    }
    if _,err:=ReadFile(filepath.Join(t.TempDir(),"missing.yaml"));err==nil{
        t.Fatal("missing manifest accepted")
    }
    if err:=os.WriteFile(path,[]byte("profile: [invalid\n"),0600);err!=nil{t.Fatal(err)}
    if _,err:=ReadFile(path);err==nil{t.Fatal("malformed YAML accepted")}
    if err:=os.WriteFile(path,[]byte(good+"---\nrepository: {name: second}\n"),0600);err!=nil{t.Fatal(err)}
    if _,err:=ReadFile(path);err==nil{t.Fatal("multiple YAML documents accepted")}
}

func TestManifestValidationRulesTable(t *testing.T){
    valid:=func()Manifest{return Manifest{
        Repository:Repository{Name:"release"},Action:"ci",
        Workspace:"/opt/workspace",
        Setup:[]string{"echo ready"},
    }}
    cases:=[]struct{name string;change func(*Manifest);want string}{
        {"invalid profile",func(m *Manifest){m.Profile="../escape"},"profile:"},
        {"root SSH user",func(m *Manifest){m.SSH.User="root"},"ssh.user"},
        {"unsafe SSH user",func(m *Manifest){m.SSH.User="admin;whoami"},"ssh.user"},
        {"missing repository",func(m *Manifest){m.Repository.Name=""},"repository:"},
        {"invalid action",func(m *Manifest){m.Action="../escape"},"action:"},
        {"relative workspace",func(m *Manifest){m.Workspace="../data"},"workspace"},
        {"newline workspace",func(m *Manifest){m.Workspace="/root\nother"},"workspace"},
        {"no setup or pipelines",func(m *Manifest){m.Setup=nil},"at least one"},
        {"missing action",func(m *Manifest){m.Action=""},"action is required"},
        {"bad setup command",func(m *Manifest){m.Setup=[]string{" "}},"setup"},
        {"NUL setup command",func(m *Manifest){m.Setup=[]string{"echo\x00bad"}},"setup"},
        {"invalid import source",func(m *Manifest){m.Imports=[]ImportSpec{{Source:""}}},"imports"},
        {"newline import source",func(m *Manifest){m.Imports=[]ImportSpec{{Source:"bad\nfile"}}},"imports"},
        {"unsafe pipeline name",func(m *Manifest){m.Pipelines=[]Pipeline{{Name:"../escape",Jobs:[]Job{{Name:"job",Commands:[]string{"echo ok"}}}}}},"pipeline"},
        {"pipeline without jobs",func(m *Manifest){m.Pipelines=[]Pipeline{{Name:"pipeline"}}},"no jobs"},
        {"unsafe job name",func(m *Manifest){m.Pipelines=[]Pipeline{{Name:"pipeline",Jobs:[]Job{{Name:"bad/name",Commands:[]string{"echo ok"}}}}}},"job:"},
        {"job without commands",func(m *Manifest){m.Pipelines=[]Pipeline{{Name:"pipeline",Jobs:[]Job{{Name:"job"}}}}},"no commands"},
        {"empty job command",func(m *Manifest){m.Pipelines=[]Pipeline{{Name:"pipeline",Jobs:[]Job{{Name:"job",Commands:[]string{" "}}}}}},"job"},
        {"duplicate setup job",func(m *Manifest){m.Pipelines=[]Pipeline{{Name:"pipeline",Jobs:[]Job{{Name:"setup",Commands:[]string{"echo ok"}}}}}},"duplicate"},
    }
    for _,tc:=range cases{
        t.Run(tc.name,func(t *testing.T){
            m:=valid()
            tc.change(&m)
            err:=m.Validate()
            if err==nil||!strings.Contains(err.Error(),tc.want){
                t.Fatalf("missing validation %q: %v",tc.want,err)
            }
        })
    }
}

func TestManifestGroupNameAndSeparatePipeline(t *testing.T){
    for _,tc:=range []struct{action string;pipelines []Pipeline;want string}{
        {"build",nil,"build"},
        {"",[]Pipeline{{Name:"one"}},"one"},
        {"",[]Pipeline{{Name:"one"},{Name:"two"}},""},
    }{
        m:=Manifest{Action:tc.action,Pipelines:tc.pipelines}
        if actual:=m.GroupName();actual!=tc.want{t.Fatalf("group %q want %q",actual,tc.want)}
    }
    valid:=Manifest{Repository:Repository{Name:"repo"},Pipelines:[]Pipeline{{
        Name:"only",Jobs:[]Job{{Name:"run",Commands:[]string{"true"}}},
    }}}
    if err:=valid.Validate();err!=nil{t.Fatalf("single pipeline should supply implicit group: %v",err)}
}
