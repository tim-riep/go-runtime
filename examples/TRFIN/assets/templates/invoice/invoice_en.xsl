<?xml version="1.0" encoding="UTF-8"?>
<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="html" encoding="UTF-8"/>
  <xsl:template match="/invoice">
    <html>
      <body>
        <h1>Invoice <xsl:value-of select="@number"/></h1>
        <p>Total: <xsl:value-of select="total"/></p>
      </body>
    </html>
  </xsl:template>
</xsl:stylesheet>
