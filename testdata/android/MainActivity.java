package dev.ninelives.fixture;

import android.app.Activity;
import android.app.Dialog;
import android.os.Bundle;
import android.view.View;
import android.view.accessibility.AccessibilityNodeInfo;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

/** Synthetic, offline app. All data is reset when the owned test calls reset. */
public class MainActivity extends Activity {
  /** Qualification facts the Android smoke reads with run-as, outside the
   * accessibility reads the driver uses: that the activity was created and
   * drew its content, and which watched click handlers ran. One fixed-format
   * line each, with fixed words and field lengths, never field text. reset
   * (pm clear) deletes the file with the app's other data, so it covers the
   * time since the last reset. It never changes what the app shows or does. */
  private void note(String line) {
    java.io.File file = new java.io.File(getFilesDir(), "handled");
    if (file.length() > 4096) return;
    try (java.io.FileOutputStream out = new java.io.FileOutputStream(file, true)) {
      out.write((line + "\n").getBytes(java.nio.charset.StandardCharsets.US_ASCII));
    } catch (java.io.IOException ignored) {
      // A diagnostic that could not be written is absent, never an app failure.
    }
  }

  /** The watched controls by name, for the touch facts below. */
  private final java.util.Map<String, View> watched = new java.util.LinkedHashMap<String, View>();

  /** Each touch that reached this window, by the watched control it landed on
   * (or `other`), and window focus changes: a tap that never reached the app
   * is told apart from one that landed elsewhere and from one whose handler
   * did not run. A fixed word, never a coordinate. */
  @Override public boolean dispatchTouchEvent(android.view.MotionEvent event) {
    int action = event.getActionMasked();
    String name = action == android.view.MotionEvent.ACTION_DOWN ? "down"
      : action == android.view.MotionEvent.ACTION_UP ? "up"
      : action == android.view.MotionEvent.ACTION_CANCEL ? "cancel" : null;
    if (name != null) note("touch " + name + " " + hit(event.getRawX(), event.getRawY()));
    return super.dispatchTouchEvent(event);
  }

  private String hit(float x, float y) {
    int[] at = new int[2];
    for (java.util.Map.Entry<String, View> entry : watched.entrySet()) {
      View view = entry.getValue();
      if (!view.isShown()) continue;
      view.getLocationOnScreen(at);
      if (x >= at[0] && x < at[0] + view.getWidth() && y >= at[1] && y < at[1] + view.getHeight()) return entry.getKey();
    }
    return "other";
  }

  @Override public void onWindowFocusChanged(boolean hasFocus) {
    super.onWindowFocusChanged(hasFocus);
    note(hasFocus ? "focus in" : "focus out");
  }

  @Override public void onCreate(Bundle state) {
    super.onCreate(state);
    note("create");
    if (FixtureMode.VALUE.equals("isolation")) { isolation(); return; }
    if (FixtureMode.VALUE.equals("semantics-delayed")) {
      // am start -W can finish before asynchronous app content is ready.
      // Keep the main thread responsive so reads observe the missing controls.
      new android.os.Handler(getMainLooper()).postDelayed(new Runnable() {
        @Override public void run() { semantics(); }
      }, 8000);
      return;
    }
    if (FixtureMode.VALUE.startsWith("semantics")) { semantics(); return; }
    if (FixtureMode.VALUE.equals("biometric")) { biometric(); return; }
    if (FixtureMode.VALUE.equals("focus")) { focus(); return; }
    LinearLayout layout = new LinearLayout(this);
    layout.setOrientation(LinearLayout.VERTICAL);
    layout.setPadding(32, 80, 32, 32);
    TextView heading = new TextView(this);
    heading.setText("Synthetic transfer");
    layout.addView(heading);
    EditText recipient = new EditText(this);
    recipient.setId(R.id.recipient);
    recipient.setHint("Recipient");
    layout.addView(recipient);
    EditText amount = new EditText(this);
    amount.setId(R.id.amount);
    amount.setHint("Amount");
    layout.addView(amount);
    CheckBox confirmed = new CheckBox(this);
    confirmed.setId(R.id.confirmed);
    confirmed.setText("Transfer complete");
    confirmed.setChecked(false);
    layout.addView(confirmed);
    TextView receipt = new TextView(this);
    receipt.setId(R.id.receipt);
    layout.addView(receipt);
    Button send = new Button(this);
    send.setText("Send synthetic transfer");
    if (!FixtureMode.VALUE.equals("missing")) send.setId(R.id.send);
    layout.addView(send);
    if (FixtureMode.VALUE.equals("ambiguous")) {
      Button duplicate = new Button(this);
      duplicate.setId(R.id.send);
      duplicate.setText("Duplicate send");
      layout.addView(duplicate);
    }
    watched.put("recipient", recipient);
    watched.put("amount", amount);
    watched.put("send", send);
    send.setOnClickListener(new View.OnClickListener() {
      @Override public void onClick(View view) {
      note("click send " + amount.length() + " " + recipient.length());
      if (FixtureMode.VALUE.equals("blocked")) {
        try { new java.io.File(getFilesDir(), "started").createNewFile(); Thread.sleep(30000); } catch (Exception ignored) { Thread.currentThread().interrupt(); }
      }
      String value = FixtureMode.VALUE.equals("defect") ? "6" : amount.getText().toString();
      receipt.setText("Sent $" + value + " to " + recipient.getText().toString());
      confirmed.setChecked(!FixtureMode.VALUE.equals("state-defect"));
      }
    });
    // Native operations: a status line, a button that opens a dialog in its
    // own window (Back dismisses it), and a list that reports a scroll.
    final TextView status = new TextView(this);
    status.setId(R.id.status);
    layout.addView(status);
    Button open = new Button(this);
    open.setId(R.id.open);
    open.setText("Open details");
    layout.addView(open);
    open.setOnClickListener(new View.OnClickListener() {
      @Override public void onClick(View view) {
        Dialog dialog = new Dialog(MainActivity.this);
        TextView details = new TextView(MainActivity.this);
        details.setId(R.id.dialog);
        details.setText("Synthetic details");
        details.setPadding(48, 48, 48, 48);
        dialog.setContentView(details);
        dialog.show();
      }
    });
    ScrollView list = new ScrollView(this);
    list.setId(R.id.list);
    LinearLayout lines = new LinearLayout(this);
    lines.setOrientation(LinearLayout.VERTICAL);
    for (int i = 1; i <= 60; i++) {
      TextView line = new TextView(this);
      line.setText("Synthetic line " + i);
      line.setPadding(0, 16, 0, 16);
      lines.addView(line);
    }
    list.addView(lines);
    list.setOnScrollChangeListener(new View.OnScrollChangeListener() {
      @Override public void onScrollChange(View view, int x, int y, int oldX, int oldY) { status.setText("scrolled"); }
    });
    layout.addView(list, new LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 600));
    setContentView(layout);
    note("drawn");
  }

  /** A bare accessibility identifier, as Flutter's Semantics(identifier:)
   * reports it: no package prefix and no Android resource. */
  private static void identify(View view, final String id) {
    view.setAccessibilityDelegate(new View.AccessibilityDelegate() {
      @Override public void onInitializeAccessibilityNodeInfo(View host, AccessibilityNodeInfo info) {
        super.onInitializeAccessibilityNodeInfo(host, info);
        info.setViewIdResourceName(id);
      }
    });
  }

  // The accessibility shapes a Flutter app produces: bare identifiers, a
  // field with only a hint, a merged multi-line label and a field error
  // reported as content-invalid. semantics-defect never reports the error,
  // though its status line still says the input is invalid.
  private boolean invalid = false;
  private void isolation() {
    final android.content.SharedPreferences session = getSharedPreferences("session", MODE_PRIVATE);
    LinearLayout layout = new LinearLayout(this);
    layout.setOrientation(LinearLayout.VERTICAL);
    layout.setPadding(32, 80, 32, 32);
    final TextView status = new TextView(this);
    identify(status, "session.status");
    final boolean signedIn = session.getBoolean("signed-in", false);
    status.setText(signedIn ? "Signed in" : "Signed out");
    Button signIn = new Button(this);
    identify(signIn, "sign-in");
    signIn.setText("Sign in");
    signIn.setOnClickListener(new View.OnClickListener() {
      @Override public void onClick(View view) {
        note("click sign-in");
        if (!session.edit().putBoolean("signed-in", true).commit()) throw new IllegalStateException("fixture session not saved");
        status.setText("Signed in");
      }
    });
    watched.put("sign-in", signIn);
    layout.addView(signIn);
    layout.addView(status);
    setContentView(layout);
    note(signedIn ? "drawn in" : "drawn out");
  }

  private void semantics() {
    LinearLayout layout = new LinearLayout(this);
    layout.setOrientation(LinearLayout.VERTICAL);
    layout.setPadding(32, 80, 32, 32);
    final TextView status = new TextView(this);
    identify(status, "login.status");
    final EditText email = new EditText(this);
    email.setHint("Email address");
    email.setAccessibilityDelegate(new View.AccessibilityDelegate() {
      @Override public void onInitializeAccessibilityNodeInfo(View host, AccessibilityNodeInfo info) {
        super.onInitializeAccessibilityNodeInfo(host, info);
        info.setContentInvalid(invalid);
        info.setError(invalid ? "Enter an email address" : null);
      }
    });
    layout.addView(email);
    Button validate = new Button(this);
    validate.setText("Validate");
    identify(validate, "validate");
    validate.setOnClickListener(new View.OnClickListener() {
      @Override public void onClick(View view) {
        boolean bad = !email.getText().toString().contains("@");
        note("click validate " + (bad ? "invalid" : "valid") + " " + email.length());
        // The defect shows the same status but never reports the field error.
        invalid = bad && !FixtureMode.VALUE.equals("semantics-defect");
        if (!FixtureMode.VALUE.equals("semantics-defect")) email.setError(bad ? "Enter an email address" : null);
        status.setText(bad ? "Invalid" : "Valid");
      }
    });
    layout.addView(validate);
    Button login = new Button(this);
    login.setText("Sign in");
    identify(login, "login-button");
    login.setOnClickListener(new View.OnClickListener() {
      @Override public void onClick(View view) {
        note("click login " + email.length());
        status.setText("Signed in as " + email.getText().toString());
      }
    });
    layout.addView(login);
    watched.put("email", email);
    watched.put("validate", validate);
    watched.put("login", login);
    layout.addView(status);
    TextView inbox = new TextView(this);
    inbox.setText("Inbox");
    inbox.setPadding(0, 32, 0, 32);
    inbox.setContentDescription("Inbox\n3 unread\nTab 2 of 4 (new)");
    layout.addView(inbox);
    setContentView(layout);
    note("drawn");
  }

  // Sign-in behind the system BiometricPrompt: the prompt is a system window
  // in front of the app, and only an enrolled fingerprint authenticates.
  private void biometric() {
    LinearLayout layout = new LinearLayout(this);
    layout.setOrientation(LinearLayout.VERTICAL);
    layout.setPadding(32, 80, 32, 32);
    final TextView status = new TextView(this);
    status.setText("Signed out");
    identify(status, "auth.status");
    Button signIn = new Button(this);
    signIn.setText("Sign in");
    identify(signIn, "sign-in");
    signIn.setOnClickListener(new View.OnClickListener() {
      @Override public void onClick(View view) {
        final java.util.concurrent.Executor main = getMainExecutor();
        android.hardware.biometrics.BiometricPrompt prompt = new android.hardware.biometrics.BiometricPrompt.Builder(MainActivity.this)
          .setTitle("Sign in to the synthetic app")
          .setNegativeButton("Cancel", main, new android.content.DialogInterface.OnClickListener() {
            @Override public void onClick(android.content.DialogInterface dialog, int which) { status.setText("Canceled"); }
          })
          .build();
        prompt.authenticate(new android.os.CancellationSignal(), main, new android.hardware.biometrics.BiometricPrompt.AuthenticationCallback() {
          @Override public void onAuthenticationSucceeded(android.hardware.biometrics.BiometricPrompt.AuthenticationResult result) { status.setText("Authenticated"); }
          @Override public void onAuthenticationFailed() { status.setText("Not recognized"); }
          @Override public void onAuthenticationError(int code, CharSequence message) { status.setText("Biometric error"); }
        });
      }
    });
    layout.addView(signIn);
    layout.addView(status);
    setContentView(layout);
  }

  // Checks that run on a user's focus changes and keyboard action, as forms
  // often do: the e-mail check runs when the field loses focus, and the name
  // field submits on the keyboard's done action. Text set without focus
  // triggers neither.
  private void focus() {
    LinearLayout layout = new LinearLayout(this);
    layout.setOrientation(LinearLayout.VERTICAL);
    layout.setPadding(32, 80, 32, 32);
    final TextView checked = new TextView(this);
    identify(checked, "focus.checked");
    final TextView submitted = new TextView(this);
    identify(submitted, "focus.submitted");
    final EditText email = new EditText(this);
    email.setHint("Email address");
    email.setSingleLine(true);
    identify(email, "email-field");
    email.setOnFocusChangeListener(new View.OnFocusChangeListener() {
      @Override public void onFocusChange(View view, boolean hasFocus) {
        if (!hasFocus) checked.setText(email.getText().toString().contains("@") ? "Checked: valid" : "Checked: invalid");
      }
    });
    final EditText name = new EditText(this);
    name.setHint("Name");
    name.setSingleLine(true);
    name.setImeOptions(android.view.inputmethod.EditorInfo.IME_ACTION_DONE);
    identify(name, "name-field");
    name.setOnEditorActionListener(new TextView.OnEditorActionListener() {
      @Override public boolean onEditorAction(TextView view, int action, android.view.KeyEvent event) {
        if (action != android.view.inputmethod.EditorInfo.IME_ACTION_DONE) return false;
        submitted.setText("Submitted: " + name.getText().toString());
        return false;
      }
    });
    layout.addView(email);
    layout.addView(name);
    layout.addView(checked);
    layout.addView(submitted);
    setContentView(layout);
  }
}
