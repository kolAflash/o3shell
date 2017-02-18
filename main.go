// Receive and send messages via commandline.
package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
	
	"github.com/o3ma/o3"
	"github.com/skip2/go-qrcode"
)


func main() {
	pass, idpath, abpath, gdpath, qrpath, pubNick, pubNickSet, rid, testMsg, createID := parseArgs()
	
	tr, tid, ctx, receiveMsgChan, sendMsgChan := initialise(pass, idpath, abpath, gdpath, qrpath, pubNick, pubNickSet, createID)
	
	go receiveLoop(tr, tid, abpath, gdpath, ctx, receiveMsgChan, sendMsgChan)
	
	sendTestMsg(tr, abpath, rid, testMsg, ctx, sendMsgChan)
	
	sendLoop(tr, abpath, ctx, sendMsgChan)
}


func parseArgs() ([]byte, string, string, string, string, string, bool, string, string, bool) {
	cmdlnPubNick  := flag.String("pubnick",          "parrot", "The public nickname for the account (max. 32 chars).")
	cmdlnConfdir  := flag.String("confdir",                "", "Path to the configuration directory.")
	cmdlnPass     := flag.String("pass",           "01234567", "A string which should be at least 8 chars long (else may cause problems).")
	cmdlnHexPass  := flag.String("hexpass",                "", "Like --pass but in hexidecimal. Overrides --pass option. E.g.: 4d7954696e795057")
	cmdlnTestID   := flag.String("testid",                 "", "Send \"testmsg\" to this ID (8 character hex string).")
	cmdlnTestMsg  := flag.String("testmsg",  "Say something!", "Send this message to \"testid\".")
	cmdlnCreateID := flag.Bool(  "createid",            false, "Create a new ID if nessesary without asking for confirmation.")
	flag.Parse()
	flagset := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { flagset[f.Name]=true } )
	
	var (
		pass    = []byte{0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37} // 01234567
		idpath  = "threema.id"
		abpath  = "address.book"
		gdpath  = "group.directory"
		qrpath  = "threema.id.png"
	)
	
	cmdlnPubNickVal := *cmdlnPubNick
	if len(cmdlnPubNickVal) > 32 { cmdlnPubNickVal = cmdlnPubNickVal[0:32] }
	pubNick := cmdlnPubNickVal
	var pubNickSet bool = !!flagset["pubnick"]
	
	rid := ""
	ridRegex := regexp.MustCompile("\\A[0-9A-Z]{8}\\z")
	cmdlnTestIDVal := ridRegex.FindString(strings.ToUpper(*cmdlnTestID))
	if cmdlnTestIDVal != "" && len(cmdlnTestIDVal) == 8 { rid = cmdlnTestIDVal }
	
	testMsg := *cmdlnTestMsg
	
	if flagset["hexpass"] {
		hexpass, err := hex.DecodeString(*cmdlnHexPass)
		if err == nil {
			pass = hexpass
		} else {
			fmt.Printf("  Error decoding hexpass!\n")
			os.Exit(1)
		}
	} else {
		pass = []byte(*cmdlnPass)
	}
	if utf8.RuneCountInString(string(pass)) < 8 { fmt.Print("  Warning: Password should have at least 8 characters to avoid problems with original Threema client!\n") }
	
	if (*cmdlnConfdir) != "" {
		idpath = (*cmdlnConfdir) + "/" + idpath
		abpath = (*cmdlnConfdir) + "/" + abpath
		gdpath = (*cmdlnConfdir) + "/" + gdpath
		qrpath = (*cmdlnConfdir) + "/" + qrpath
	}
	
	createId := *cmdlnCreateID
	
	return pass, idpath, abpath, gdpath, qrpath, pubNick, pubNickSet, rid, testMsg, createId
}


func initialise(pass []byte, idpath string, abpath string, gdpath string, qrpath string, pubNick string, pubNickSet bool, createID bool) (*o3.ThreemaRest, *o3.ThreemaID, *o3.SessionContext, <-chan o3.ReceivedMsg, chan<- o3.Message) {
	var (
		tr      o3.ThreemaRest
		tid     o3.ThreemaID
	)
	
	// check whether an id file exists or else create a new one
	if _, err := os.Stat(idpath); err != nil {
		if !createID {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("  No existing ID found. Create a new one? Enter YES (upper case) or NO: ")
			text, _ := reader.ReadString('\n')
			createID = (text == "YES\n")
		}
		
		if createID {
			var err error
			tid, err = tr.CreateIdentity()
			if err != nil {
				fmt.Println("  CreateIdentity failed!")
				log.Fatal(err)
			}
			fmt.Printf("  Saving ID to %s\n", idpath)
			err = tid.SaveToFile(idpath, pass)
			if err != nil {
				fmt.Println("  Saving ID failed!")
				log.Fatal(err)
			}
		} else {
			os.Exit(1)
		}
	} else {
		fmt.Printf("  Loading ID from %s\n", idpath)
		tid, err = o3.LoadIDFromFile(idpath, pass)
		if err != nil {
			log.Fatal(err)
		}
	}
	
	saveQrCode(&tid, qrpath)
	
	if pubNickSet {
		tid.Nick = o3.NewPubNick(pubNick)
		fmt.Printf("  Setting public nickname to: %s\n", pubNick)
	}
	
	fmt.Printf("  Using ID %s and public nickname: %s\n", tid.String(), tid.Nick)
	
	ctx := o3.NewSessionContext(tid)
	
	//check if we can load an addressbook
	if _, err := os.Stat(abpath); !os.IsNotExist(err) {
		fmt.Printf("  Loading addressbook from: %s\n", abpath)
		err = ctx.ID.Contacts.LoadFromFile(abpath)
		if err != nil {
			fmt.Println("  Loading addressbook failed!")
			log.Fatal(err)
		}
	}

	//check if we can load a group directory
	if _, err := os.Stat(gdpath); !os.IsNotExist(err) {
		fmt.Printf("  Loading group directory from: %s\n", gdpath)
		err = ctx.ID.Groups.LoadFromFile(gdpath)
		if err != nil {
			fmt.Println("  Loading group directory failed!")
			log.Fatal(err)
		}
	}
	
	// let the session begin
	fmt.Println("  Starting session")
	sendMsgChan, receiveMsgChan, err := ctx.Run()
	if err != nil {
		log.Fatal(err)
	}
	
	return &tr, &tid, &ctx, receiveMsgChan, sendMsgChan
}


func sendTestMsg(tr *o3.ThreemaRest, abpath string, rid string, testMsg string, ctx *o3.SessionContext, sendMsgChan chan<- o3.Message) {
	// check if we know the remote ID for
	// (just demonstration purposes \bc sending and receiving functions do this lookup for us)
	if rid != "" {
		contact := addContactIfUnknown(tr, abpath, ctx, rid, "")
		
		// send our initial message to our recipient
		err, tm := ctx.SendTextMessage(rid, testMsg, sendMsgChan)
		fmt.Println("  Sending initial message [" + fmt.Sprintf("%x", tm.ID()) + "] to " + contactToS(contact, false) + ": " + testMsg + "\n--------------------\n")
		if err != nil {
			log.Fatal(err)
		}
	}
}


func receiveLoop(tr *o3.ThreemaRest, tid *o3.ThreemaID, abpath string, gdpath string, ctx *o3.SessionContext, receiveMsgChan <-chan o3.ReceivedMsg, sendMsgChan chan<- o3.Message) {
	// handle incoming messages
	for receivedMessage := range receiveMsgChan {
		if receivedMessage.Err != nil {
			fmt.Printf("  Error Receiving Message: %s\n", receivedMessage.Err)
			continue
		}
		switch msg := receivedMessage.Msg.(type) {
		case o3.ImageMessage:
			// display the image if you like
			fmt.Printf("  Image Message from %s. (displaying image messages not implemented yet)\n", msg.Sender())
		case o3.AudioMessage:
			// play the audio if you like
			fmt.Printf("  Audio Message from %s. (playing audio messages not implemented yet)\n", msg.Sender())
		case o3.TextMessage:
			// Print text message.
			fmt.Printf("\nMessage from %s: %s: %s\n--------------------\n\n", msg.Sender(), msg.PubNick(), msg.Text())
			confirmMsg(ctx, &msg, sendMsgChan)
			updateContact(tr, abpath, ctx, &msg)
		case o3.GroupTextMessage:
			fmt.Printf("  %s for Group [%x] created by [%s]:\n%s\n", msg.Sender(), msg.GroupID(), msg.GroupCreator(), msg.Text())
			group, ok := ctx.ID.Groups.Get(msg.GroupCreator(), msg.GroupID())
			if ok {
				ctx.SendGroupTextMessage(group, "This is a group reply!", sendMsgChan)
			}
		case o3.GroupManageSetNameMessage:
			fmt.Printf("  Group [%x] is now called %s\n", msg.GroupID(), msg.Name())
			ctx.ID.Groups.Upsert(o3.Group{CreatorID: msg.Sender(), GroupID: msg.GroupID(), Name: msg.Name()})
			ctx.ID.Groups.SaveToFile(gdpath)
		case o3.GroupManageSetMembersMessage:
			fmt.Printf("  Group [%x] now includes %v\n", msg.GroupID(), msg.Members())
			ctx.ID.Groups.Upsert(o3.Group{CreatorID: msg.Sender(), GroupID: msg.GroupID(), Members: msg.Members()})
			ctx.ID.Groups.SaveToFile(gdpath)
		case o3.GroupMemberLeftMessage:
			fmt.Printf("  Member [%s] left the Group [%x]\n", msg.Sender(), msg.GroupID())
		case o3.DeliveryReceiptMessage:
			fmt.Printf("  Message [%x] has been acknowledged by the server.\n", msg.MsgID())
		case o3.TypingNotificationMessage:
			fmt.Printf("  Typing Notification from %s: [%x]\n", msg.Sender(), msg.OnOff)
		default:
			fmt.Printf("  Unknown message type from: %s\nContent: %#v", msg.Sender(), msg)
		}
	}
}


func sendLoop(tr *o3.ThreemaRest, abpath string, ctx *o3.SessionContext, sendMsgChan chan<- o3.Message) {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Println("  Sending thread startet! Send messages via: ---ID---MESSAGE (e.g. 1337ABCDHello World!)")
	for {
		rid_msg, _ := reader.ReadString('\n')
		idValid := false
		if len(rid_msg) >= 8 {
			rid := rid_msg[0:8]
			msg := rid_msg[8:len(rid_msg)-1]
			
			ridRegex := regexp.MustCompile("\\A[0-9A-Z]{8}\\z")
			rid = ridRegex.FindString(strings.ToUpper(rid))
			if rid != "" && len(rid) == 8 {
				idValid = true
				
				
				contact := addContactIfUnknown(tr, abpath, ctx, rid, "")
				
				err, tm := ctx.SendTextMessage(rid, msg, sendMsgChan)
				fmt.Println("  Sending message [" + fmt.Sprintf("%x", tm.ID()) + "] to " + contactToS(contact, false) + ".\n")
				if err != nil {
					log.Fatal(err)
				}
			}
		}
		
		if !idValid {
			fmt.Println("  ID is invalid!")
		}
	}
}


func confirmMsg(ctx *o3.SessionContext, msg *o3.TextMessage, sendMsgChan chan<- o3.Message) {
	// confirm to the sender that we received the message
	// this is how one can send messages manually without helper functions like "SendTextMessage"
	drm, err := o3.NewDeliveryReceiptMessage(ctx, msg.Sender().String(), msg.ID(), o3.MSGDELIVERED)
	if err != nil {
		log.Fatal(err)
	}
	sendMsgChan <- drm
}


func addContactIfUnknown(tr *o3.ThreemaRest, abpath string, ctx *o3.SessionContext, rid string, pubNick string) o3.ThreemaContact {
	contact, contactFound := ctx.ID.Contacts.Get(rid); 
	if contactFound == false {
		contact = addContact(tr, abpath, ctx, rid, "")
		fmt.Printf("  Contact missing in addressbook, so add it. ID: %s, Nickname: %s\n", rid, pubNick)
	}
	return contact
}


func addContact(tr *o3.ThreemaRest, abpath string, ctx *o3.SessionContext, rid string, pubNick string) o3.ThreemaContact {
	//retrieve the ID from Threema's servers
	myID := o3.NewIDString(rid)
	fmt.Printf("  Retrieving %s from directory server\n", myID.String())
	contact, err := tr.GetContactByID(myID)
	contact.PubNick = pubNick
	if err != nil {
		log.Fatal(err)
	}
	// add them to our address book
	ctx.ID.Contacts.Add(contact)
	saveAddressbook(abpath, ctx)
	return contact
}


func updateContact(tr *o3.ThreemaRest, abpath string, ctx *o3.SessionContext, msg *o3.TextMessage) {
	rid := msg.Sender().String()
	contact, contactFound := ctx.ID.Contacts.Get(rid)
	newPubNick := fmt.Sprintf("%s", msg.PubNick().String())
	
	if !contactFound {
		contact = addContact(tr, abpath, ctx, rid, newPubNick)
		fmt.Printf("  Contact missing in addressbook, so add it. ID: %s, Nickname: %s\n", rid, newPubNick)
	} else if (contact.PubNick != newPubNick) {
		fmt.Println("  Contact " + contactToS(contact, true) + " has new public nickname: " + newPubNick)
		contact.PubNick = newPubNick
		ctx.ID.Contacts.Add(contact)
		saveAddressbook(abpath, ctx)
	}
}


func saveAddressbook(abpath string, ctx *o3.SessionContext) {
	//and save the address book
	//fmt.Printf("  Saving addressbook to: %s\n", abpath)
	err := ctx.ID.Contacts.SaveTo(abpath)
	if err != nil {
		fmt.Println("  Saving addressbook failed!")
		log.Fatal(err)
	}
}


func contactToS(contact o3.ThreemaContact, withPubNick bool) string {
	result := "\"" + contact.PubNick + "\""
	
	if contact.FirstName != "" || contact.LastName != "" {
		space := ""
		if contact.FirstName != "" && contact.LastName != "" { space = " " }
		firstlast := contact.FirstName + space + contact.LastName
		if withPubNick && result != "" {
			result = firstlast + " " + result
		} else {
			result = firstlast
		}
	}
	
	space := ""
	if result != "" { space = " " }
	result =  "[" + contact.ID.String() + "]" + space + result
	
	return result
}


func saveQrCode(tid *o3.ThreemaID, qrpath string) {
	if _, err := os.Stat(qrpath); err != nil {
		// No qr code file found. Generating one.
		// concat QR-Code content. "3mid" maybe is a shortcut for Threema-ID
		qrtext := fmt.Sprintf("3mid:%s,%x", tid.String(), tid.GetPubKey()[:])

		// generate the PNG-Image
		err := qrcode.WriteFile(qrtext, qrcode.Medium, 256, qrpath)
		if err != nil { 
			log.Fatal(err)
		}
	}
}

