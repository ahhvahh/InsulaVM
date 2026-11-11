package insulavm

import (
 "context"
 "strings"
 "testing"
)

func TestCreateProfileFromISORejectsBadDestinationBeforeProvisioning(t *testing.T) {
 e:=testEngine(t,"")
 for _,dest:=range []string{"relative/guest","/","/tmp/..","/tmp/guest\x00bad"}{
  _,err:=e.CreateProfileFromISO(context.Background(),CreateProfileFromISORequest{
   Name:"invalid-destination",GuestDestination:dest,
  })
  if err==nil||!strings.Contains(err.Error(),"guest destination"){
   t.Fatalf("unsafe destination %q allowed before ISO validation: %v",dest,err)
  }
 }
}
