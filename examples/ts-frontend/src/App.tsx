import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { UserSchema } from "./gen/user/v1/user.type";
import { useState } from "react";
import { z } from "zod";

// Use the Zod-inferred type so React Hook Form's resolver is happy.
// This is the *same* type as the generated User interface.
type UserForm = z.infer<typeof UserSchema>;

// The key insight: UserSchema is generated from your .proto file.
// Same constraints as the Go backend — email, length, range, enum.
// One source of truth, two languages, zero drift.

export function App() {
  const [result, setResult] = useState<{ ok: boolean; body: string } | null>(
    null,
  );

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<UserForm>({
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    resolver: zodResolver(UserSchema) as any,
    defaultValues: {
      email: "",
      displayName: "",
      age: 0,
      role: "USER_ROLE_UNSPECIFIED",
      bio: null,
    },
  });

  const onSubmit = async (data: UserForm) => {
    // Convert role string back to numeric for the Go API
    const roleMap: Record<string, number> = {
      USER_ROLE_UNSPECIFIED: 0,
      USER_ROLE_MEMBER: 1,
      USER_ROLE_ADMIN: 2,
    };
    const body = {
      email: data.email,
      display_name: data.displayName,
      age: data.age,
      role: roleMap[data.role] ?? 0,
      bio: data.bio || undefined,
    };

    try {
      const res = await fetch("/users", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const json = await res.json();
      setResult({ ok: res.ok, body: JSON.stringify(json, null, 2) });
    } catch (e) {
      setResult({ ok: false, body: String(e) });
    }
  };

  return (
    <div style={{ maxWidth: 480, margin: "40px auto", fontFamily: "system-ui" }}>
      <h1>Create User</h1>
      <p style={{ color: "#666", fontSize: 14 }}>
        Form validated by a Zod schema generated from{" "}
        <code>user.proto</code> — same constraints as the Go backend.
      </p>

      <form onSubmit={handleSubmit(onSubmit)}>
        <Field label="Email" error={errors.email?.message}>
          <input {...register("email")} type="email" placeholder="alice@example.com" />
        </Field>

        <Field label="Display Name" error={errors.displayName?.message}>
          <input {...register("displayName")} placeholder="Alice" />
        </Field>

        <Field label="Age" error={errors.age?.message}>
          <input
            {...register("age", { valueAsNumber: true })}
            type="number"
            placeholder="25"
          />
        </Field>

        <Field label="Role" error={errors.role?.message}>
          <select {...register("role")}>
            <option value="USER_ROLE_UNSPECIFIED">Unspecified</option>
            <option value="USER_ROLE_MEMBER">Member</option>
            <option value="USER_ROLE_ADMIN">Admin</option>
          </select>
        </Field>

        <Field label="Bio (optional)" error={errors.bio?.message}>
          <textarea {...register("bio")} rows={3} placeholder="Tell us about yourself..." />
        </Field>

        <button
          type="submit"
          style={{
            marginTop: 16,
            padding: "8px 24px",
            background: "#2563eb",
            color: "white",
            border: "none",
            borderRadius: 6,
            cursor: "pointer",
            fontSize: 14,
          }}
        >
          Create User
        </button>
      </form>

      {result && (
        <pre
          style={{
            marginTop: 24,
            padding: 16,
            background: result.ok ? "#f0fdf4" : "#fef2f2",
            border: `1px solid ${result.ok ? "#86efac" : "#fca5a5"}`,
            borderRadius: 8,
            fontSize: 13,
            overflow: "auto",
          }}
        >
          {result.ok ? "✅ " : "❌ "}
          {result.body}
        </pre>
      )}
    </div>
  );
}

function Field({
  label,
  error,
  children,
}: {
  label: string;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <div style={{ marginBottom: 16 }}>
      <label style={{ display: "block", marginBottom: 4, fontWeight: 500, fontSize: 14 }}>
        {label}
      </label>
      {children}
      {error && (
        <p style={{ color: "#dc2626", fontSize: 13, margin: "4px 0 0" }}>{error}</p>
      )}
      <style>{`
        input, select, textarea {
          width: 100%;
          padding: 8px;
          border: 1px solid #d1d5db;
          border-radius: 6px;
          font-size: 14px;
          box-sizing: border-box;
        }
      `}</style>
    </div>
  );
}
