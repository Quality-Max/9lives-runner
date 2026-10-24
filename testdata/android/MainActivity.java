package dev.ninelives.fixture;

import android.app.Activity;
import android.os.Bundle;
import android.view.View;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.TextView;

/** Synthetic, offline app. All data is reset when the owned test calls reset. */
public class MainActivity extends Activity {
  @Override public void onCreate(Bundle state) {
    super.onCreate(state);
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
    send.setOnClickListener(new View.OnClickListener() {
      @Override public void onClick(View view) {
      if (FixtureMode.VALUE.equals("blocked")) {
        try { new java.io.File(getFilesDir(), "started").createNewFile(); Thread.sleep(30000); } catch (Exception ignored) { Thread.currentThread().interrupt(); }
      }
      String value = FixtureMode.VALUE.equals("defect") ? "6" : amount.getText().toString();
      receipt.setText("Sent $" + value + " to " + recipient.getText().toString());
      confirmed.setChecked(!FixtureMode.VALUE.equals("state-defect"));
      }
    });
    setContentView(layout);
  }
}
