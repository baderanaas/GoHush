package libp2p

import (
	"encoding/json"
	"os"
	"sync"
)

// Contact represents a user's contact with a name and PeerID.
type Contact struct {
	Name   string `json:"name"`
	PeerID string `json:"peerId"`
}

// ContactManager manages the contacts.
type ContactManager struct {
	contacts []Contact
	lock     sync.RWMutex
	filePath string
}

// NewContactManager creates a new ContactManager.
func NewContactManager(filePath string) (*ContactManager, error) {
	cm := &ContactManager{
		filePath: filePath,
	}
	if err := cm.LoadContacts(); err != nil {
		return nil, err
	}
	return cm, nil
}

// LoadContacts loads contacts from the JSON file.
func (cm *ContactManager) LoadContacts() error {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	file, err := os.ReadFile(cm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			cm.contacts = []Contact{}
			return nil
		}
		return err
	}

	return json.Unmarshal(file, &cm.contacts)
}

// SaveContacts saves contacts to the JSON file.
func (cm *ContactManager) SaveContacts() error {
	cm.lock.RLock()
	defer cm.lock.RUnlock()

	file, err := json.MarshalIndent(cm.contacts, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cm.filePath, file, 0644)
}

// AddContact adds a new contact.
func (cm *ContactManager) AddContact(name, peerID string) {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	cm.contacts = append(cm.contacts, Contact{Name: name, PeerID: peerID})
}

// GetContact returns a contact by name.
func (cm *ContactManager) GetContact(name string) (Contact, bool) {
	cm.lock.RLock()
	defer cm.lock.RUnlock()

	for _, contact := range cm.contacts {
		if contact.Name == name {
			return contact, true
		}
	}
	return Contact{}, false
}

// GetContactByPeerID returns a contact by peer ID.
func (cm *ContactManager) GetContactByPeerID(peerID string) (Contact, bool) {
	cm.lock.RLock()
	defer cm.lock.RUnlock()

	for _, contact := range cm.contacts {
		if contact.PeerID == peerID {
			return contact, true
		}
	}
	return Contact{}, false
}

// ListContacts returns all contacts.
func (cm *ContactManager) ListContacts() []Contact {
	cm.lock.RLock()
	defer cm.lock.RUnlock()

	return cm.contacts
}
