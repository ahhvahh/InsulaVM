package profile

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestProfileValidationBoundaryMatrix(t *testing.T){
    valid:=func()Profile{return Profile{
        Name:"debian",Version:"v001",MemoryMiB:512,VCPUs:1,DiskGB:8,
        Image:"versions/v001/base.qcow2",Directory:t.TempDir(),
    }}
    cases:=[]struct{name string;change func(*Profile);want string}{
        {"name",func(p *Profile){p.Name="../escape"},"profile.name"},
        {"memory low",func(p *Profile){p.MemoryMiB=127},"memory_mib"},
        {"memory high",func(p *Profile){p.MemoryMiB=1048577},"memory_mib"},
        {"cpu low",func(p *Profile){p.VCPUs=0},"vcpus"},
        {"cpu high",func(p *Profile){p.VCPUs=257},"vcpus"},
        {"disk",func(p *Profile){p.DiskGB=0},"disk_gb"},
        {"version",func(p *Profile){p.Version=""},"profile.version"},
        {"missing image",func(p *Profile){p.Image=""},"profile.image"},
        {"absolute image",func(p *Profile){p.Image="/tmp/unsafe.qcow2"},"profile.image"},
        {"image NUL",func(p *Profile){p.Image="test\x00file"},"profile.image"},
        {"image traversal",func(p *Profile){p.Image="../../escape.qcow2"},"profile.image"},
    }
    for _,tc:=range cases{
        t.Run(tc.name,func(t *testing.T){
            p:=valid()
            tc.change(&p)
            if err:=p.Validate();err==nil||!strings.Contains(err.Error(),tc.want){
                t.Fatalf("expected %s error, got %v",tc.want,err)
            }
        })
    }
}

func TestProfileReadRejectsMalformedMetadata(t *testing.T){
    root:=t.TempDir()
    dir:=filepath.Join(root,"debian")
    if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
    meta:=filepath.Join(dir,"profile.yaml")
    tests:=[]struct{name,value string}{
        {"unknown fields","name: debian\nimage: base.qcow2\nunexpected: true\n"},
        {"mismatched name","name: other\nversion: v001\nimage: base.qcow2\nmemory_mib: 512\nvcpus: 1\ndisk_gb: 8\n"},
        {"invalid YAML","name: [bad\n"},
        {"two docs","name: debian\n---\nname: other\n"},
    }
    for _,tc:=range tests{
        t.Run(tc.name,func(t *testing.T){
            if err:=os.WriteFile(meta,[]byte(tc.value),0600);err!=nil{t.Fatal(err)}
            if _,err:=Read(root,"debian");err==nil{
                t.Fatal("untrusted profile metadata accepted")
            }
        })
    }
    if err:=os.Remove(meta);err!=nil{t.Fatal(err)}
    if _,err:=Read(root,"debian");err==nil{t.Fatal("missing metadata accepted")}
    if err:=os.Mkdir(meta,0700);err!=nil{t.Fatal(err)}
    if _,err:=Read(root,"debian");err==nil{t.Fatal("directory as profile metadata accepted")}
}

func TestProfileImagePathChecksQCOW2MagicAndOwnership(t *testing.T){
    root:=t.TempDir()
    path:=filepath.Join(root,"base.qcow2")
    p:=Profile{
        Name:"debian",Version:"v001",Image:"base.qcow2",
        MemoryMiB:512,VCPUs:1,DiskGB:8,Directory:root,
    }
    if _,err:=p.ImagePath();err==nil{t.Fatal("missing QCOW2 image accepted")}
    if err:=os.WriteFile(path,[]byte("invalid-image"),0600);err!=nil{t.Fatal(err)}
    if _,err:=p.ImagePath();err==nil||!strings.Contains(err.Error(),"QCOW2"){
        t.Fatalf("non-QCOW2 accepted: %v",err)
    }
    if err:=os.WriteFile(path,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
    if _,err:=p.ImagePath();err!=nil{t.Fatal(err)}
    p.Directory=""
    if _,err:=p.ImagePath();err==nil{t.Fatal("unrooted QCOW2 path accepted")}
}

func TestProfileRegisterRejectsInvalidSourceAndDuplicate(t *testing.T){
    root:=t.TempDir()
    bad:=filepath.Join(t.TempDir(),"bad.qcow2")
    if err:=os.WriteFile(bad,[]byte("not a qcow"),0600);err!=nil{t.Fatal(err)}
    p:=Profile{Name:"debian",MemoryMiB:512,VCPUs:1,DiskGB:8}
    if err:=Register(root,p,bad);err==nil{t.Fatal("invalid QCOW2 source accepted")}
    if err:=Register(root,p,filepath.Join(t.TempDir(),"missing.qcow2"));err==nil{
        t.Fatal("missing source accepted")
    }
    source:=filepath.Join(t.TempDir(),"disk.qcow2")
    qcow2(t,source)
    if err:=Register(root,p,source);err!=nil{t.Fatal(err)}
    if err:=Register(root,p,source);err==nil{t.Fatal("profile overwritten")}
    names,err:=List(root)
    if err!=nil||len(names)!=1||names[0]!="debian"{t.Fatalf("registered profile missing: %v %v",names,err)}
}
