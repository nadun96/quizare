# School sign-in (Active Directory and LDAP)

Students and teachers can sign in with their school account, the one they use on school computers, instead of an account made here (D-64). It works with Windows Active Directory and with other LDAP directories such as OpenLDAP. An admin sets it up in **Admin → School sign-in**.

## What changes for people

- **The login page** asks for "Email or school username". Students and teachers type their Windows name (`asilva`), their `user@domain` name or their email, and their school password.
- **The first sign-in creates the account.** Its name and email come from the directory, and its role from the directory's groups: members of the teachers group are teachers, members of the students group are students. Email verification isn't needed: the directory vouches for the address.
- **Existing accounts move over.** A student or teacher who already had an account here with the same email keeps it, with their classrooms and results. Their old password stops working. An account with a different role isn't moved: a student account doesn't become a teacher's by email alone. You can switch this off ("Existing accounts: Leave them").
- **Passwords belong to the school.** School accounts can't reset or change their password here. The forgot-password and account pages say so.
- **Admins and managers keep their own password**, and never sign in through the directory. If the directory is down or misconfigured, an admin can still get in and turn school sign-in off.
- **Accounts with their own password keep working** alongside school accounts, for example a visiting teacher.

## Setting it up in Active Directory

1. **Create a service account** that can read users and groups, for example `svc-quiz` in an *OU=Service Accounts*. Ordinary domain users can read the directory, so it needs no extra rights. Give it a long password that doesn't expire, or note when it does.
2. **Make sure LDAPS works.** Domain controllers serve LDAPS on port 636 once they have a certificate (from AD Certificate Services or another CA). Without one, use StartTLS on port 389; it needs a certificate too. If the certificate comes from your own CA, paste the CA certificate (PEM) in **Certificate authority**.
3. **Choose the groups.** For example *CN=Staff,OU=Groups,DC=school,DC=local* for teachers and *CN=Students,OU=Groups,…* for students. With **Groups inside the groups: Count too**, members of *Year 10* count as students when *Year 10* is inside *Students*.
4. **Fill in Admin → School sign-in**: the server (`ldaps://dc1.school.local`), the service account's DN and password, and where to look for people (*OU=People,DC=school,DC=local*, or the whole domain *DC=school,DC=local*).
5. **Test with a real account.** The test shows each step: connecting, the service account signing in, finding the person, checking their password, and the role their groups give. Test a teacher and a student.
6. **Turn it on.** The login page changes for everyone at once.

Disabled accounts can't sign in: the default filter leaves them out (`userAccountControl` bit 2).

## Other LDAP directories

Choose **Other LDAP**. People sign in with their `uid` or email; the id is `entryUUID` and the name `cn`. Group membership is read from `memberOf`, so the directory needs the memberOf overlay (OpenLDAP) or an equivalent. Some LDAP servers allow searching without a service account; leave it empty then.

## The hourly check

Students stay signed in for up to 30 days (ADR-13). So that someone who leaves the school doesn't keep access for that long, the server checks every signed-in school account once an hour. Accounts that were deleted, disabled, moved out of the groups, or given another role in the directory are signed out. If the directory can't be reached, nobody is signed out: an outage at the school shouldn't end every lesson.

## Security

- **Always encrypted.** Only `ldaps://`, or `ldap://` with StartTLS, is accepted, and the server's certificate is always checked (the system's CAs plus the one you paste). There is no option to skip the check.
- **No empty passwords.** An LDAP bind with a name and an empty password is an "unauthenticated bind", which Active Directory accepts for any name. Empty passwords are refused before the directory is asked.
- **No filter injection.** What people type is escaped before it goes into the search filter (`*`, `(`, `)`, `\` and NUL), and the search must find exactly one account.
- **The service account's password** is sealed with the master key, like teachers' LLM keys (ADR-09), and is never sent back to the browser.
- **Only admins** can see or change these settings; they decide who becomes a teacher, so they can't be given to a manager.
- **Every change is audited**: settings changes, accounts created, moved over, given another role by the directory, or signed out by the hourly check.

## Troubleshooting

| The test says | Likely cause |
|---|---|
| *Connect securely* fails with a certificate error | The server's certificate isn't trusted, or its name doesn't match the address. Paste your CA's certificate, and use the name in the certificate (not the IP address). |
| *Connect securely* times out | Port 636 (or 389 for StartTLS) is blocked between this server and the domain controller. |
| *Sign the service account in* fails | Wrong DN or password, or the account is locked or expired. |
| *Find …* finds no account | The person is outside *Where to look for people*, or the filter doesn't match how they typed their name. |
| Not in the teacher group or the student group | Check the group DNs, and whether the person is in a group inside the group (turn on *Groups inside the groups*). |
| No email address | The account has no `mail` and no valid `userPrincipalName`. Add an email in the directory. |
