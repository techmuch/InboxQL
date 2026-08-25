import socket
import threading
import time

# Sample Emails
EMAILS = [
    {
        "id": 1,
        "from": "alice@tech.com",
        "to": "admin@inboxql.local",
        "subject": "Project X Status",
        "body": "The synchronization logic for Project X is 90% complete. We should be able to launch the beta next week.",
        "date": "Wed, 25 Feb 2026 10:00:00 +0000",
        "flags": ["\\Seen"]
    },
    {
        "id": 2,
        "from": "bob@security.io",
        "to": "admin@inboxql.local",
        "subject": "Security Audit Result",
        "body": "The latest audit of the IMAP sync engine passed with no high-severity vulnerabilities.",
        "date": "Thu, 26 Feb 2026 14:30:00 +0000",
        "flags": []
    },
    {
        "id": 3,
        "from": "marketing@travel.com",
        "to": "admin@inboxql.local",
        "subject": "Flight to San Francisco",
        "body": "Your flight UA123 is confirmed. Departure: March 5th, 8:00 AM.",
        "date": "Fri, 27 Feb 2026 09:15:00 +0000",
        "flags": []
    }
]

class MockIMAPServer:
    def __init__(self, host='0.0.0.0', port=3143):
        self.host = host
        self.port = port
        self.server_socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.server_socket.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)

    def start(self):
        self.server_socket.bind((self.host, self.port))
        self.server_socket.listen(5)
        print(f"Mock IMAP server started on {self.host}:{self.port}")
        while True:
            client_socket, addr = self.server_socket.accept()
            print(f"Connection from {addr}")
            threading.Thread(target=self.handle_client, args=(client_socket,)).start()

    def handle_client(self, client_socket):
        client_socket.send(b"* OK [CAPABILITY IMAP4rev1 AUTH=PLAIN] Mock IMAP Server Ready\r\n")
        
        while True:
            try:
                data = client_socket.recv(4096).decode('utf-8').strip()
                if not data:
                    break
                
                print(f"C: {data}")
                parts = data.split(' ')
                tag = parts[0]
                if len(parts) < 2:
                    continue
                cmd = parts[1].upper()

                if cmd == "CAPABILITY":
                    client_socket.send(f"* CAPABILITY IMAP4rev1 AUTH=PLAIN\r\n{tag} OK CAPABILITY completed\r\n".encode())
                elif cmd == "LOGIN":
                    client_socket.send(f"{tag} OK LOGIN completed\r\n".encode())
                elif cmd == "LIST":
                    client_socket.send(b'* LIST (\\HasNoChildren) "/" "INBOX"\r\n')
                    client_socket.send(b'* LIST (\\HasNoChildren) "/" "Sent"\r\n')
                    client_socket.send(f"{tag} OK LIST completed\r\n".encode())
                elif cmd == "SELECT":
                    client_socket.send(f"* {len(EMAILS)} EXISTS\r\n".encode())
                    client_socket.send(b"* 0 RECENT\r\n")
                    client_socket.send(b"* OK [UIDVALIDITY 1] UIDs valid\r\n")
                    client_socket.send(f"{tag} OK [READ-WRITE] SELECT completed\r\n".encode())
                elif cmd == "FETCH":
                    for i, email in enumerate(EMAILS):
                        # Create a proper RFC822 message
                        msg_body = f"From: {email['from']}\r\nTo: {email['to']}\r\nSubject: {email['subject']}\r\nDate: {email['date']}\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n{email['body']}"
                        envelope = f'("{email["date"]}" "{email["subject"]}" (("Name" NIL "{email["from"].split("@")[0]}" "{email["from"].split("@")[1]}")) NIL NIL (("To" NIL "{email["to"].split("@")[0]}" "{email["to"].split("@")[1]}")) NIL NIL NIL "{email["id"]}@inboxql.local")'
                        resp = f"* {i+1} FETCH (UID {email['id']} FLAGS ({' '.join(email['flags'])}) INTERNALDATE \"{email['date']}\" RFC822.SIZE {len(msg_body)} ENVELOPE {envelope} BODY[] {{{len(msg_body)}}}\r\n{msg_body})\r\n"
                        client_socket.send(resp.encode())
                    client_socket.send(f"{tag} OK FETCH completed\r\n".encode())
                elif cmd == "LOGOUT":
                    client_socket.send(b"* BYE Mock IMAP server logging out\r\n")
                    client_socket.send(f"{tag} OK LOGOUT completed\r\n".encode())
                    break
                else:
                    client_socket.send(f"{tag} OK {cmd} ignored\r\n".encode())
            except Exception as e:
                print(f"Error: {e}")
                break
        
        client_socket.close()

if __name__ == "__main__":
    server = MockIMAPServer()
    server.start()
