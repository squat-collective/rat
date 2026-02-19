import { redirect } from "next/navigation";

/**
 * Community edition login stub.
 * No auth configured — always redirect home.
 * In Pro builds, this file is replaced by the auth-keycloak plugin overlay.
 */
export default function LoginPage() {
  redirect("/");
}
