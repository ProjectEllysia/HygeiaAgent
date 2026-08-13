package config

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// buildSDDL construye la DACL restringida. Concede acceso total solo a:
//
//	SY   LocalSystem (la cuenta bajo la que corre el servicio instalado)
//	BA   Administradores
//	<propietario del proceso actual>
//
// D:P marca la DACL como protegida: NO hereda del directorio padre. Sin eso,
// las ACEs de C:\ProgramData (que conceden lectura al grupo Users) se
// reañadirían y la clave de agente volvería a ser legible por cualquiera.
//
// El propietario del proceso entra en la lista porque el agente también se
// ejecuta en primer plano durante el desarrollo, bajo una cuenta normal: sin
// su ACE se quedaría sin acceso a su propia config. Cuando corre como
// servicio el propietario ES LocalSystem y el ACE simplemente se duplica,
// que es inofensivo.
//
// `inherit` añade OICI para que lo que se cree dentro de un directorio nazca
// ya restringido.
func buildSDDL(inherit bool) (string, error) {
	flags := ""
	if inherit {
		flags = "OICI"
	}

	sddl := fmt.Sprintf("D:P(A;%s;GA;;;SY)(A;%s;GA;;;BA)", flags, flags)

	sid, err := currentUserSID()
	if err != nil {
		return "", err
	}
	if sid != "" {
		sddl += fmt.Sprintf("(A;%s;GA;;;%s)", flags, sid)
	}
	return sddl, nil
}

// currentUserSID devuelve el SID del propietario del proceso.
func currentUserSID() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("config: obteniendo el usuario del proceso: %w", err)
	}
	return user.User.Sid.String(), nil
}

// secure aplica la DACL restringida a una ruta.
//
// En Windows el modo 0600 de os.WriteFile es esencialmente un no-op: el
// fichero hereda la ACL del padre, y la de C:\ProgramData concede lectura al
// grupo Users. Sin esto, la clave de agente quedaría legible por cualquier
// usuario local, justo lo que el §5 prohíbe.
func secure(path string, inherit bool) error {
	sddl, err := buildSDDL(inherit)
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("config: SDDL inválido: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("config: extrayendo DACL: %w", err)
	}

	// PROTECTED_DACL_SECURITY_INFORMATION corta la herencia: si no, las ACEs
	// heredadas del padre se volverían a añadir a las nuestras.
	err = windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
	if err != nil {
		return fmt.Errorf("config: aplicando ACL a %q: %w", path, err)
	}
	return nil
}

// securePath restringe un fichero (la config con la clave de agente).
func securePath(path string) error { return secure(path, false) }

// secureDir restringe el directorio de estado y lo que se cree dentro.
func secureDir(path string) error { return secure(path, true) }
